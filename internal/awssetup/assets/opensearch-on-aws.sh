#!/usr/bin/env bash
# opensearch-on-aws.sh: provision a dedicated OpenSearch node on AWS for rag-cli and
# configure the local rag-cli against it. Exported by
#   rag-cli.rag prepare-script aws --output <dir>
# Usage: ./opensearch-on-aws.sh setup | destroy        (see docs/opensearch-on-aws.md)
#
# Runs outside snap confinement with your own AWS CLI, Terraform, ssh and sudo.
# settings.env and secrets.env are read as data (never sourced) and may be edited.
set -euo pipefail
umask 077

DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
TF_DIR=$DIR/terraform
SETTINGS=$DIR/settings.env
SECRETS=$DIR/secrets.env
CONTEXT=$DIR/snap-context.env
SSH_KEY=$DIR/ssh/id_ed25519
KNOWN_HOSTS=$DIR/ssh/known_hosts
LOG_DIR=$DIR/logs

SETTINGS_KEYS=(AWS_PROFILE AWS_REGION INSTANCE_TYPE UBUNTU_RELEASE ROOT_VOLUME_GIB AMI_ID
	OPENSEARCH_CHANNEL ALLOWED_CIDR DEPLOYMENT_ID CHAT_HOST CHAT_PORT CHAT_PATH CHAT_TLS CHAT_MODEL
	DRIVE_IMPORT DRIVE_FOLDER_URL DRIVE_IMPORT_LAST_ATTEMPT)
SECRET_KEYS=(OPENSEARCH_ADMIN_PASSWORD TLS_ROOT_PASS TLS_ADMIN_PASS TLS_NODE_PASS CHAT_API_KEY
	GOOGLE_DRIVE_CLIENT_ID GOOGLE_DRIVE_CLIENT_SECRET)
# Values redacted from output. The Google OAuth client ID is deliberately absent: it is a
# public identifier that appears in the authorization URL the importer prints, and masking it
# breaks the sign-in URL. It is still stored and configured exactly like the other secrets.
MASKED_KEYS=(OPENSEARCH_ADMIN_PASSWORD TLS_ROOT_PASS TLS_ADMIN_PASS TLS_NODE_PASS CHAT_API_KEY
	GOOGLE_DRIVE_CLIENT_SECRET)
GENERATED_SECRETS=(OPENSEARCH_ADMIN_PASSWORD TLS_ROOT_PASS TLS_ADMIN_PASS TLS_NODE_PASS)
CANONICAL_OWNER=099720109477

declare -A CFG=()
PHASE=start
PHASE_LOG=

# --- output and failure reporting -------------------------------------------

mask() {
	local line k
	while IFS= read -r line || [ -n "$line" ]; do
		for k in "${MASKED_KEYS[@]}"; do
			[ -n "${CFG[$k]:-}" ] && line=${line//"${CFG[$k]}"/***}
		done
		printf '%s\n' "$line"
	done
}

phase() {
	PHASE=$1
	PHASE_LOG=$LOG_DIR/$2.log
	mkdir -p "$LOG_DIR"
	: >"$PHASE_LOG"
	printf '\n==> %s\n' "$1"
}

die() {
	printf '\nerror in phase "%s": %s\n' "$PHASE" "$*" | mask >&2
	if [ -n "$PHASE_LOG" ] && [ -s "$PHASE_LOG" ]; then
		printf -- '--- last lines of %s\n' "$PHASE_LOG" >&2
		tail -n 25 "$PHASE_LOG" >&2
	fi
	exit 1
}

# run <cmd...>: show and log the command's output (masked); fail the phase on error.
run() {
	local rc=0
	"$@" 2>&1 | mask | tee -a "$PHASE_LOG" || rc=$?
	[ "$rc" = 0 ] || die "command failed ($rc): $1 ${2:-}"
}

say() { printf '%s\n' "$*"; }

confirm() { # confirm <question>; true on y/yes
	local a
	read -rp "$1 [y/N]: " a
	[[ $a =~ ^[Yy]([Ee][Ss])?$ ]]
}

# --- saved answers (data only, never sourced) --------------------------------

has() { [ "${CFG[$1]+set}" = set ]; }

# Format: KEY="value" with \" and \\ as the only escapes (what save_env writes).
# Hand-written KEY=value (unquoted) and KEY='value' are taken literally.
load_env() { # load_env <file> <allowed keys...>
	local file=$1 n=0 line key val dq='^"(([^"\\]|\\["\\])*)"$' sq="^'([^']*)'\$"
	shift
	local -A allowed=()
	for key in "$@"; do allowed[$key]=1; done
	[ -f "$file" ] || return 0
	while IFS= read -r line || [ -n "$line" ]; do
		n=$((n + 1))
		[[ $line =~ ^[[:space:]]*(#|$) ]] && continue
		[[ $line =~ ^([A-Z][A-Z0-9_]*)=(.*)$ ]] || die "$file line $n: expected KEY=value"
		key=${BASH_REMATCH[1]} val=${BASH_REMATCH[2]}
		[ -n "${allowed[$key]:-}" ] || die "$file line $n: unknown key $key"
		if [[ $val =~ $dq ]]; then
			val=${BASH_REMATCH[1]}
			val=${val//\\\\/$'\001'}
			val=${val//\\\"/\"}
			val=${val//$'\001'/\\}
		elif [[ $val =~ $sq ]]; then
			val=${BASH_REMATCH[1]}
		elif [[ $val == [\"\']* ]]; then
			die "$file line $n: unbalanced quotes in the value of $key"
		fi
		CFG[$key]=$val
	done <"$file"
}

save_env() { # save_env <file> <keys...>
	local file=$1 tmp key
	shift
	tmp=$(mktemp "$file.XXXXXX")
	local v
	for key in "$@"; do
		has "$key" || continue
		v=${CFG[$key]//\\/\\\\}
		printf '%s="%s"\n' "$key" "${v//\"/\\\"}" >>"$tmp"
	done
	mv -f "$tmp" "$file"
}

save_all() {
	save_env "$SETTINGS" "${SETTINGS_KEYS[@]}"
	save_env "$SECRETS" "${SECRET_KEYS[@]}"
}

valid() { # valid <key> <value>
	local v=$2
	[[ $v =~ [[:cntrl:]] ]] && return 1
	case $1 in
	AWS_PROFILE) [[ $v =~ ^[A-Za-z0-9_.@+-]+$ ]] ;;
	AWS_REGION) [[ $v =~ ^[a-z]{2}(-[a-z]+)+-[0-9]+$ ]] ;;
	INSTANCE_TYPE) [[ $v =~ ^[a-z0-9]+\.[a-z0-9]+$ ]] ;;
	UBUNTU_RELEASE) [[ $v == 24.04 || $v == 26.04 ]] ;;
	ROOT_VOLUME_GIB) [[ $v =~ ^[0-9]+$ ]] && ((10#$v >= 20 && 10#$v <= 16384)) ;;
	AMI_ID) [[ $v =~ ^ami-[0-9a-f]{8,17}$ ]] ;;
	OPENSEARCH_CHANNEL) [[ $v =~ ^[0-9]+/(stable|candidate|beta|edge)$ ]] ;;
	ALLOWED_CIDR) [[ $v =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}/32$ ]] ;;
	CHAT_HOST) [[ $v =~ ^[A-Za-z0-9.-]+$ ]] ;;
	CHAT_PORT) [[ $v =~ ^[0-9]+$ ]] && ((10#$v >= 1 && 10#$v <= 65535)) ;;
	CHAT_PATH) [[ $v =~ ^[A-Za-z0-9._/-]*$ ]] ;;
	CHAT_TLS) [[ $v == true || $v == false ]] ;;
	DRIVE_IMPORT) [[ $v == yes || $v == no ]] ;;
	DRIVE_FOLDER_URL) [[ $v == https://drive.google.com/* ]] ;;
	CHAT_API_KEY) true ;;
	# Written verbatim into credentials.json and curl config lines.
	OPENSEARCH_ADMIN_PASSWORD | TLS_*_PASS) [ -n "$v" ] && [[ $v != *[\"\\]* ]] ;;
	*) [ -n "$v" ] ;;
	esac
}

# ask <key> <question> [default] [secret]: prompt unless a valid value is saved.
ask() {
	local key=$1 q=$2 def=${3-} secret=${4-} ans
	if has "$key" && valid "$key" "${CFG[$key]}"; then return 0; fi
	has "$key" && say "The saved value of $key is invalid; please enter it again."
	while :; do
		if [ -n "$secret" ]; then
			read -rsp "$q: " ans
			echo
		else
			read -rp "$q${def:+ [$def]}: " ans
			ans=${ans:-$def}
		fi
		valid "$key" "$ans" && break
		say "Invalid value for $key."
	done
	CFG[$key]=$ans
}

gen_secret() {
	local s
	s=$(head -c 512 /dev/urandom | LC_ALL=C tr -dc 'A-Za-z0-9')
	echo "${s:0:32}"
}

# --- tools and context ------------------------------------------------------

need() { command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"; }

load_context() {
	[ "$(id -u)" != 0 ] || die "run this script as your normal user, not with sudo; it calls sudo itself where needed"
	load_env "$CONTEXT" RAG_SNAP_INSTANCE RAG_SNAP_USER_COMMON
	if ! has RAG_SNAP_INSTANCE || ! has RAG_SNAP_USER_COMMON; then
		die "$CONTEXT is missing; rerun: rag-cli.rag prepare-script aws --output $DIR"
	fi
	local home
	home=$(getent passwd "$(id -un)" | cut -d: -f6)
	[ "${CFG[RAG_SNAP_USER_COMMON]}" = "$home/snap/${CFG[RAG_SNAP_INSTANCE]}/common" ] ||
		die "$CONTEXT records ${CFG[RAG_SNAP_USER_COMMON]}, which is not $home/snap/${CFG[RAG_SNAP_INSTANCE]}/common; rerun 'rag-cli.rag prepare-script aws' as $(id -un) without sudo"
	RAG=${CFG[RAG_SNAP_INSTANCE]}.rag
	load_env "$SETTINGS" "${SETTINGS_KEYS[@]}"
	load_env "$SECRETS" "${SECRET_KEYS[@]}"
}

aws_() { aws --profile "${CFG[AWS_PROFILE]}" --region "${CFG[AWS_REGION]:-us-east-1}" "$@"; }

# profile_configured reports whether the named profile exists in the AWS
# configuration. When the AWS CLI cannot list profiles, assume it exists so an
# existing profile is never overwritten.
profile_configured() {
	local out
	if out=$(aws configure list-profiles 2>/dev/null); then
		grep -qxF "$1" <<<"$out"
	else
		return 0
	fi
}

# resolve_identity verifies AWS authentication before any other deployment
# question. A profile that does not exist is offered 'aws configure'; an existing
# profile whose credentials are rejected keeps its configuration and gets
# authentication guidance instead. 'aws configure' runs interactively on the
# user's terminal: this script never captures, pipes, logs or saves its prompts.
resolve_identity() {
	phase "AWS identity" identity
	ask AWS_PROFILE "AWS CLI profile" default
	save_all

	local arn
	if arn=$(aws_ sts get-caller-identity --query Arn --output text 2>&1); then
		say "AWS identity: $arn"
		return 0
	fi
	if profile_configured "${CFG[AWS_PROFILE]}"; then
		die "AWS rejected the credentials for profile '${CFG[AWS_PROFILE]}': $arn
Refresh them the way you set them up (for example 'aws sso login --profile ${CFG[AWS_PROFILE]}', a new session token, or 'aws configure --profile ${CFG[AWS_PROFILE]}'), then rerun ./opensearch-on-aws.sh $ACTION"
	fi
	say "Profile '${CFG[AWS_PROFILE]}' is not configured."
	say "The next step runs 'aws configure --profile ${CFG[AWS_PROFILE]}', which stores an access key ID and secret access key in your AWS configuration."
	say "If you use SSO, an instance role, or another authentication method, answer no, configure it yourself, and rerun."
	confirm "Run 'aws configure --profile ${CFG[AWS_PROFILE]}' now?" ||
		die "configure profile '${CFG[AWS_PROFILE]}' yourself, then rerun ./opensearch-on-aws.sh $ACTION"
	aws configure --profile "${CFG[AWS_PROFILE]}" ||
		die "'aws configure --profile ${CFG[AWS_PROFILE]}' failed; configure the profile and rerun"
	arn=$(aws_ sts get-caller-identity --query Arn --output text 2>&1) ||
		die "profile '${CFG[AWS_PROFILE]}' is still not usable: $arn"
	say "AWS identity: $arn"
}

tf() { terraform -chdir="$TF_DIR" "$@"; }

tf_vars() {
	TF_VARS=(-var "aws_profile=${CFG[AWS_PROFILE]}" -var "region=${CFG[AWS_REGION]}"
		-var "deployment_id=${CFG[DEPLOYMENT_ID]}" -var "instance_type=${CFG[INSTANCE_TYPE]}"
		-var "ami_id=${CFG[AMI_ID]}" -var "root_volume_gib=${CFG[ROOT_VOLUME_GIB]}"
		-var "allowed_cidr=${CFG[ALLOWED_CIDR]}" -var "ssh_public_key_path=$SSH_KEY.pub")
}

protect_state() { chmod 600 "$TF_DIR"/terraform.tfstate* "$TF_DIR"/*.tfplan 2>/dev/null || true; }

# --- setup phases -----------------------------------------------------------

preflight() {
	phase "Preflight" preflight
	local c
	for c in curl ssh ssh-keygen scp sudo snap getent; do need "$c"; done
	command -v "$RAG" >/dev/null 2>&1 || die "$RAG not found; install the ${CFG[RAG_SNAP_INSTANCE]} snap"
	if [ "$(snap services "${CFG[RAG_SNAP_INSTANCE]}.ragd" 2>/dev/null | awk 'NR==2{print $3}')" = active ]; then
		die "${CFG[RAG_SNAP_INSTANCE]}.ragd is running, so rag-cli commands would be served by the daemon, whose secrets this script does not manage (see INSTALL.md, Secrets). Stop it with 'sudo snap stop ${CFG[RAG_SNAP_INSTANCE]}.ragd' and rerun."
	fi
	if ! command -v aws >/dev/null 2>&1; then
		confirm "The AWS CLI is not installed. Install it with 'sudo snap install aws-cli --classic'?" ||
			die "install the AWS CLI (https://docs.aws.amazon.com/cli/) and rerun"
		sudo snap install aws-cli --classic
		hash -r
		need aws
	fi
	ensure_terraform
}

# Terraform and its providers stay external to rag-cli: an installed Terraform is
# reused, otherwise the classic snap is offered, as with the AWS CLI.
ensure_terraform() {
	if command -v terraform >/dev/null 2>&1; then
		check_terraform_version
		return 0
	fi
	confirm "Terraform is not installed. Install it with 'sudo snap install terraform --classic'?" ||
		die "install Terraform yourself (https://developer.hashicorp.com/terraform/install) and rerun"
	sudo snap install terraform --classic ||
		die "could not install the Terraform snap; install Terraform from https://developer.hashicorp.com/terraform/install and rerun"
	hash -r
	command -v terraform >/dev/null 2>&1 ||
		die "the Terraform snap installed but 'terraform' is not on PATH; open a new shell (or add /snap/bin to PATH) and rerun"
	check_terraform_version
}

# check_terraform_version enforces the supported range used by the templates.
check_terraform_version() {
	local v major minor rest
	v=$(terraform version 2>/dev/null | sed -n '1s/^Terraform v\([0-9][0-9.]*\).*/\1/p') || true
	[ -n "$v" ] || die "'terraform version' did not report a version; check that the Terraform binary works"
	major=${v%%.*}
	rest=${v#*.}
	minor=${rest%%.*}
	if [ "$major" != 1 ] || [ "$minor" -lt 6 ]; then
		die "Terraform $v is not supported; this setup needs 1.6 or later (and below 2.0)"
	fi
	say "terraform: $(command -v terraform) v$v"
}

collect_answers() {
	resolve_identity

	phase "Deployment settings" settings
	ask AWS_REGION "AWS region" us-east-1
	ask INSTANCE_TYPE "EC2 instance type" t3.xlarge
	ask UBUNTU_RELEASE "Ubuntu release (24.04 or 26.04)" 24.04
	ask ROOT_VOLUME_GIB "Root volume size in GiB" 50
	ask OPENSEARCH_CHANNEL "OpenSearch snap channel" 2/stable
	has DEPLOYMENT_ID || CFG[DEPLOYMENT_ID]=rag-$(od -An -N4 -tx1 /dev/urandom | tr -d ' \n')

	local ip
	ip=$(curl -4fsS --max-time 10 https://checkip.amazonaws.com | tr -d '[:space:]') || ip=
	[[ $ip =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || ip=
	if has ALLOWED_CIDR && valid ALLOWED_CIDR "${CFG[ALLOWED_CIDR]}" && [ -n "$ip" ] && [ "${CFG[ALLOWED_CIDR]}" != "$ip/32" ]; then
		confirm "Your public IPv4 is now $ip; update the allowed address from ${CFG[ALLOWED_CIDR]}?" && CFG[ALLOWED_CIDR]=$ip/32
	fi
	ask ALLOWED_CIDR "Public IPv4 /32 allowed to reach SSH and OpenSearch" "${ip:+$ip/32}"

	say "Inference endpoint (an existing OpenAI-compatible server, e.g. AWS Bedrock):"
	ask CHAT_HOST "Inference host" "$(rag_get chat.http.host bedrock-runtime."${CFG[AWS_REGION]}".amazonaws.com)"
	ask CHAT_PORT "Inference port" "$(rag_get chat.http.port 443)"
	ask CHAT_PATH "Inference base path" "$(rag_get chat.http.path openai/v1)"
	ask CHAT_TLS "Use TLS (true/false)" "$(rag_get chat.http.tls true)"
	ask CHAT_MODEL "Model name" "$(rag_get chat.model '')"
	has CHAT_API_KEY || ask CHAT_API_KEY "Inference API key (Enter for none)" "" secret

	ask DRIVE_IMPORT "Import knowledge bases from a Google Drive folder? (yes/no)"
	if [ "${CFG[DRIVE_IMPORT]}" = yes ]; then
		ask DRIVE_FOLDER_URL "Google Drive folder URL"
		ask GOOGLE_DRIVE_CLIENT_ID "Google OAuth client ID (Desktop app)"
		ask GOOGLE_DRIVE_CLIENT_SECRET "Google OAuth client secret" "" secret
	fi

	ensure_generated_secrets
	ensure_ssh_key
	save_all
}

# ensure_generated_secrets creates the deployment's own secrets when they are absent,
# so a redeploy after destroy mints fresh ones.
ensure_generated_secrets() {
	local k
	for k in "${GENERATED_SECRETS[@]}"; do
		[ -n "${CFG[$k]:-}" ] || CFG[$k]=$(gen_secret)
		valid "$k" "${CFG[$k]}" || die "$k in $SECRETS must not contain double quotes or backslashes; edit or remove it and rerun"
	done
}

# ensure_ssh_key generates the deployment's SSH key once.
ensure_ssh_key() {
	if [ ! -f "$SSH_KEY" ]; then
		mkdir -p "$DIR/ssh"
		ssh-keygen -q -t ed25519 -N '' -C "${CFG[DEPLOYMENT_ID]}" -f "$SSH_KEY"
	fi
}

rag_get() { # rag_get <key> <fallback>: current rag-cli value, else fallback
	local v
	v=$("$RAG" get "$1" 2>/dev/null) || v=
	echo "${v:-$2}"
}

# ami_name_filter <release>: Canonical's image-name pattern for the release,
# verified against Canonical's "Find Ubuntu images on AWS" documentation
# (ubuntu/images/hvm-ssd-gp3/ubuntu-<codename>-<release>-amd64-server-*): the
# hvm-ssd-gp3 component is the HVM/EBS-gp3 image and the codename is noble or
# resolute for the two supported releases.
ami_name_filter() {
	case $1 in
	24.04) echo 'ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*' ;;
	26.04) echo 'ubuntu/images/hvm-ssd-gp3/ubuntu-resolute-26.04-amd64-server-*' ;;
	*) return 1 ;;
	esac
}

select_ami() {
	phase "Ubuntu image" ami
	local rel=${CFG[UBUNTU_RELEASE]} ami info id arch name filter err
	if has AMI_ID && valid AMI_ID "${CFG[AMI_ID]}"; then
		ami=${CFG[AMI_ID]}
	else
		filter=$(ami_name_filter "$rel") || die "no Canonical image pattern for Ubuntu $rel"
		# DescribeImages restricted to Canonical's account; the newest match wins.
		if ! ami=$(aws_ ec2 describe-images --owners "$CANONICAL_OWNER" \
			--filters "Name=name,Values=$filter" "Name=architecture,Values=x86_64" \
			"Name=virtualization-type,Values=hvm" "Name=root-device-type,Values=ebs" \
			"Name=state,Values=available" \
			--query 'Images | sort_by(@, &CreationDate) | [-1].ImageId' --output text 2>"$PHASE_LOG.ami-err"); then
			err=$(cat "$PHASE_LOG.ami-err" 2>/dev/null)
			rm -f "$PHASE_LOG.ami-err"
			die "could not look up the Canonical Ubuntu $rel image in ${CFG[AWS_REGION]}: ${err:-no error output from the AWS CLI}"
		fi
		rm -f "$PHASE_LOG.ami-err"
		if [ -z "$ami" ] || [ "$ami" = None ] || ! valid AMI_ID "$ami"; then
			die "no available Canonical Ubuntu $rel amd64 HVM gp3 image in ${CFG[AWS_REGION]}"
		fi
	fi
	if ! info=$(aws_ ec2 describe-images --owners "$CANONICAL_OWNER" --image-ids "$ami" \
		--query 'Images[0].[ImageId,Architecture,Name]' --output text 2>"$PHASE_LOG.ami-err"); then
		err=$(cat "$PHASE_LOG.ami-err" 2>/dev/null)
		rm -f "$PHASE_LOG.ami-err"
		die "could not verify image $ami in ${CFG[AWS_REGION]}: ${err:-no error output from the AWS CLI}"
	fi
	rm -f "$PHASE_LOG.ami-err"
	read -r id arch name <<<"$info"
	if [ "$id" != "$ami" ] || [ "$arch" != x86_64 ]; then
		die "$ami is not a Canonical amd64 image in ${CFG[AWS_REGION]}; clear AMI_ID in settings.env to select a new image"
	fi
	[[ $name == *"-$rel-"* ]] || die "the saved image $ami ($name) is not Ubuntu $rel; clear AMI_ID in settings.env to select a new image"
	if [ "${CFG[AMI_ID]:-}" != "$ami" ]; then
		CFG[AMI_ID]=$ami
		save_all
	fi
	say "image: $ami ($name)"
}

provision() {
	phase "Terraform" terraform
	tf_vars
	run tf init -input=false -no-color
	local rc=0
	tf plan -input=false -no-color -detailed-exitcode -out=tfplan "${TF_VARS[@]}" >>"$PHASE_LOG" 2>&1 || rc=$?
	protect_state
	case $rc in
	0) say "no infrastructure changes" ;;
	2)
		tf show -no-color tfplan
		local a
		read -rp "Apply this plan? Type 'yes' to continue: " a
		[ "$a" = yes ] || die "plan not applied (nothing changed); rerun when ready"
		run tf apply -input=false -no-color tfplan
		protect_state
		;;
	*) die "terraform plan failed" ;;
	esac
	IP=$(tf output -raw public_ip)
	[[ $IP =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || die "no public IP in Terraform outputs"
	say "OpenSearch instance: $IP"
}

# shellcheck disable=SC2029 # remote commands are built locally on purpose; they carry no secrets
remote() { ssh "${SSH_OPTS[@]}" "ubuntu@$IP" "$@"; }

# wait_cloud_init: cloud-init must finish successfully (exit 0) before secrets are
# copied. Its detailed status goes to the phase log, whose tail die() shows.
wait_cloud_init() {
	local rc=0 out
	out=$(timeout 900 ssh "${SSH_OPTS[@]}" "ubuntu@$IP" 'cloud-init status --wait --long' 2>&1) || rc=$?
	printf '%s\n' "$out" | mask >>"$PHASE_LOG"
	case $rc in
	0) return 0 ;;
	124) die "cloud-init did not finish on $IP within 900 s" ;;
	*)
		die "cloud-init on $IP did not complete successfully (exit $rc); inspect it with: ssh ubuntu@$IP cloud-init status --long"
		;;
	esac
}

bootstrap() {
	phase "OpenSearch bootstrap on $IP" bootstrap
	SSH_OPTS=(-i "$SSH_KEY" -o IdentitiesOnly=yes -o BatchMode=yes -o ConnectTimeout=10
		-o UserKnownHostsFile="$KNOWN_HOSTS" -o StrictHostKeyChecking=accept-new)
	local _ ok=
	for _ in $(seq 1 30); do
		remote true 2>>"$PHASE_LOG" && { ok=1; break; }
		sleep 10
	done
	[ -n "$ok" ] || die "SSH to ubuntu@$IP did not succeed within 300 s"
	wait_cloud_init
	run remote 'mkdir -p -m 700 ~/rag-bootstrap'
	run scp -q "${SSH_OPTS[@]}" "$DIR/bootstrap-opensearch.sh" "ubuntu@$IP:rag-bootstrap/bootstrap-opensearch.sh"
	local k
	for k in "${GENERATED_SECRETS[@]}"; do printf '%s=%s\n' "$k" "${CFG[$k]}"; done |
		remote 'umask 077; cat > ~/rag-bootstrap/secrets.env' || die "could not copy the guest secrets file"
	run remote "sudo bash ~/rag-bootstrap/bootstrap-opensearch.sh --node-name rag0 --channel '${CFG[OPENSEARCH_CHANNEL]}' --secrets-file ~/rag-bootstrap/secrets.env"

	local code
	code=$(printf 'user = "admin:%s"\n' "${CFG[OPENSEARCH_ADMIN_PASSWORD]}" |
		curl -sk --max-time 15 -o /dev/null -w '%{http_code}' -K - "https://$IP:9200/") || true
	[ "$code" = 200 ] || die "authenticated request from this machine to https://$IP:9200/ returned HTTP ${code:-none}"
	say "remote endpoint authenticated (HTTP 200)"
}

write_credentials() {
	local dir=${CFG[RAG_SNAP_USER_COMMON]} f mode tmp existing key
	f=$dir/credentials.json
	[ -d "$dir" ] && [ -O "$dir" ] || die "$dir must exist and be owned by $(id -un); run any rag-cli.rag command once and rerun"
	mode=$(stat -c %a "$dir")
	((8#$mode & 8#022)) && die "$dir is writable by group or others; fix its permissions and rerun"
	if [ -f "$f" ]; then
		existing=$(sed -n 's/.*"OPENSEARCH_PASSWORD": *"\([^"]*\)".*/\1/p' "$f")
		if [ "$existing" != "${CFG[OPENSEARCH_ADMIN_PASSWORD]}" ]; then
			confirm "$f already holds different OpenSearch credentials. Replace it?" ||
				die "kept the existing $f; export OPENSEARCH_USERNAME/OPENSEARCH_PASSWORD yourself or rerun and allow the replacement"
		fi
	fi
	key=${CFG[CHAT_API_KEY]:-}
	key=${key//\\/\\\\}
	key=${key//\"/\\\"}
	tmp=$(mktemp "$dir/.credentials.json.XXXXXX")
	{
		printf '{\n  "OPENSEARCH_USERNAME": "admin",\n  "OPENSEARCH_PASSWORD": "%s"' "${CFG[OPENSEARCH_ADMIN_PASSWORD]}"
		[ -n "$key" ] && printf ',\n  "CHAT_API_KEY": "%s"' "$key"
		printf '\n}\n'
	} >"$tmp"
	mv -f "$tmp" "$f"
	say "wrote $f"
}

rag_set() { # rag_set <key> <value>: package-scope set, then verify the effective value
	sudo "$RAG" set --package "$1=$2" || die "sudo $RAG set --package $1=... failed"
	local got
	got=$("$RAG" get "$1" 2>/dev/null) || got=
	[ "$got" = "$2" ] || die "rag-cli $1 is '$got' after setting '$2': a user-layer value overrides it; run 'sudo $RAG set $1=$2' and rerun"
}

configure_local() {
	phase "Local rag-cli configuration" local
	write_credentials
	rag_set knowledge.http.host "$IP"
	rag_set knowledge.http.port 9200
	rag_set knowledge.http.tls true
	rag_set tika.http.host 127.0.0.1
	rag_set tika.http.port 9998
	rag_set tika.http.path tika
	rag_set chat.http.host "${CFG[CHAT_HOST]}"
	rag_set chat.http.port "${CFG[CHAT_PORT]}"
	rag_set chat.http.path "${CFG[CHAT_PATH]}"
	rag_set chat.http.tls "${CFG[CHAT_TLS]}"
	rag_set chat.model "${CFG[CHAT_MODEL]}"
	sudo snap start --enable "${CFG[RAG_SNAP_INSTANCE]}.tika-server" >/dev/null
	local _
	for _ in $(seq 1 24); do
		curl -fsS --max-time 5 -o /dev/null http://127.0.0.1:9998/tika && { say "Tika is up"; return 0; }
		sleep 5
	done
	die "Tika did not answer on http://127.0.0.1:9998/tika within 120 s (see: sudo snap logs ${CFG[RAG_SNAP_INSTANCE]}.tika-server)"
}

# model_id <Embedding|Rerank> <file>: the ID printed by `knowledge init`.
model_id() {
	tr -d '\r' <"$2" | sed 's/\x1b\[[0-9;?]*[A-Za-z]//g' |
		sed -nE "s/.*$1 model ID: ([A-Za-z0-9_-]+)[[:space:]]*$/\1/p" | tail -n 1
}

init_models() {
	phase "Knowledge models and pipelines" init
	run "$RAG" knowledge init
	local emb rer
	emb=$(model_id Embedding "$PHASE_LOG")
	rer=$(model_id Rerank "$PHASE_LOG")
	[ -n "$emb" ] && [ -n "$rer" ] || die "knowledge init did not report both model IDs"
	rag_set knowledge.model.embedding "$emb"
	rag_set knowledge.model.rerank "$rer"
}

drive_import() {
	[ "${CFG[DRIVE_IMPORT]}" = yes ] || return 0
	phase "Google Drive import" drive
	if [ -n "${CFG[DRIVE_IMPORT_LAST_ATTEMPT]:-}" ]; then
		confirm "A previous import attempt finished at ${CFG[DRIVE_IMPORT_LAST_ATTEMPT]}. Run the import again?" || return 0
	fi
	cat <<-'EOF'
		Google sign-in uses a local callback on 127.0.0.1 on this machine. If no browser opens here:
		  a) on the machine with the browser, first run: ssh -L <port>:127.0.0.1:<port> <you>@<this-host>
		     (<port> is in the redirect_uri of the printed URL), then open the URL there; or
		  b) open the URL anywhere, finish consent, copy the address of the failed
		     http://127.0.0.1:<port>/?... page and run: curl '<that address>' on this machine
		     within 5 minutes.
	EOF
	GOOGLE_DRIVE_CLIENT_ID=${CFG[GOOGLE_DRIVE_CLIENT_ID]} GOOGLE_DRIVE_CLIENT_SECRET=${CFG[GOOGLE_DRIVE_CLIENT_SECRET]} \
		run "$RAG" knowledge import --url "${CFG[DRIVE_FOLDER_URL]}" --all
	say "Import command finished; review its output for archive failures."
	CFG[DRIVE_IMPORT_LAST_ATTEMPT]=$(date -u +%Y-%m-%dT%H:%M:%SZ)
	save_all
}

setup() {
	preflight
	collect_answers
	select_ami
	provision
	bootstrap
	configure_local
	init_models
	drive_import
	phase "Summary" summary
	cat <<-EOF
		OpenSearch:  https://$IP:9200 (user admin; password in $SECRETS)
		Credentials: ${CFG[RAG_SNAP_USER_COMMON]}/credentials.json (used by rag-cli.rag from any shell)
		TLS:         traffic is encrypted, but rag-cli does not verify the server certificate.
		Try:         $RAG k create default && $RAG chat
		Remove:      $DIR/opensearch-on-aws.sh destroy
	EOF
	"$RAG" status || true
}

destroy() {
	phase "AWS identity" identity
	[ -f "$TF_DIR/terraform.tfstate" ] || die "no Terraform state in $TF_DIR; nothing to destroy"
	has AWS_PROFILE || die "AWS_PROFILE missing from $SETTINGS"
	resolve_identity
	phase "Terraform destroy" destroy
	tf_vars
	run tf init -input=false -no-color
	# A completed destroy leaves an empty state behind (and removes the SSH key the plan reads).
	[ -n "$(tf state list 2>/dev/null)" ] || die "Terraform state in $TF_DIR holds no resources; nothing to destroy"
	[ -f "$SSH_KEY.pub" ] || die "$SSH_KEY.pub is missing but Terraform still tracks resources; restore it (any ed25519 public key will do for a destroy) and rerun"
	run tf plan -destroy -input=false -no-color -out=destroy.tfplan "${TF_VARS[@]}"
	protect_state
	local a
	read -rp "Destroy every resource above? Type the deployment ID (${CFG[DEPLOYMENT_ID]}) to confirm: " a
	[ "$a" = "${CFG[DEPLOYMENT_ID]}" ] || die "not confirmed; nothing destroyed"
	run tf apply -input=false -no-color destroy.tfplan
	protect_state

	phase "Local cleanup" cleanup
	local f=${CFG[RAG_SNAP_USER_COMMON]}/credentials.json
	if [ -f "$f" ]; then
		if [ "$(sed -n 's/.*"OPENSEARCH_PASSWORD": *"\([^"]*\)".*/\1/p' "$f")" = "${CFG[OPENSEARCH_ADMIN_PASSWORD]:-}" ]; then
			rm -f "$f"
			say "removed $f"
		else
			say "kept $f: it holds credentials this deployment did not write"
		fi
	fi
	# Deployment-specific material goes; reusable user credentials and the saved
	# answers stay, so the next setup can redeploy without re-entering them.
	unset 'CFG[OPENSEARCH_ADMIN_PASSWORD]' 'CFG[TLS_ROOT_PASS]' 'CFG[TLS_ADMIN_PASS]' 'CFG[TLS_NODE_PASS]'
	unset 'CFG[DRIVE_IMPORT_LAST_ATTEMPT]'
	save_env "$SECRETS" "${SECRET_KEYS[@]}"
	save_env "$SETTINGS" "${SETTINGS_KEYS[@]}"
	rm -rf "$DIR/ssh" "$TF_DIR/tfplan" "$TF_DIR/destroy.tfplan"
	say "removed the generated secrets, the SSH key and the plan files"
	say "kept the inference API key, the Google OAuth client, the cached Google token and the saved answers"
	say "kept settings.env, Terraform state and logs with DRIVE_IMPORT_LAST_ATTEMPT cleared"
	say "the next setup generates fresh deployment secrets and a new SSH key"
	say "rag-cli configuration is unchanged: knowledge.http.host still points at the destroyed address."
}

main() {
	ACTION=${1:-}
	case $ACTION in
	setup | destroy) ;;
	*)
		echo "usage: $0 setup|destroy" >&2
		exit 2
		;;
	esac
	load_context
	"$ACTION"
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
	main "$@"
fi
