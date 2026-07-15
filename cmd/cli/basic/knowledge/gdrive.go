package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const driveAPIBase = "https://www.googleapis.com/drive/v3/files"

// ArchiveStem derives a knowledge-base name from an archive filename by
// stripping the archive extension and any "-export" suffix, mirroring the name
// the CLI derives during Drive import.
func ArchiveStem(name string) string {
	name = strings.TrimSuffix(name, ".tar.gz")
	name = strings.TrimSuffix(name, ".tgz")
	name = strings.TrimSuffix(name, "-export")
	return name
}

// DriveArchive describes a .tar.gz file discovered in a Google Drive folder.
type DriveArchive struct {
	ID   string
	Name string
	// Size is the file size in bytes as reported by the Drive API, or -1 when unavailable.
	Size int64
	// Modified is the file's last-modified time (RFC 3339) as reported by the
	// Drive API, or "" when unavailable.
	Modified string
}

// DriveURLKind distinguishes a Drive folder URL from a single-file URL.
type DriveURLKind int

const (
	DriveKindFolder DriveURLKind = iota
	DriveKindFile
)

var (
	// /drive/folders/ID  and  /drive/u/N/folders/ID
	reDriveFolder = regexp.MustCompile(`drive\.google\.com/drive/(?:u/\d+/)?folders/([a-zA-Z0-9_-]+)`)
	// /file/d/ID/...
	reDriveFileD = regexp.MustCompile(`drive\.google\.com/file/d/([a-zA-Z0-9_-]+)`)
	// /uc?id=ID  and  /open?id=ID
	reDriveIDParam = regexp.MustCompile(`drive\.google\.com/(?:uc|open)\?.*\bid=([a-zA-Z0-9_-]+)`)
)

// ParseDriveURL extracts the resource kind and ID from a Google Drive URL.
// Supported forms:
//
//	https://drive.google.com/drive/folders/FOLDER_ID
//	https://drive.google.com/drive/u/0/folders/FOLDER_ID
//	https://drive.google.com/file/d/FILE_ID/view
//	https://drive.google.com/uc?id=FILE_ID&export=download
//	https://drive.google.com/open?id=FILE_ID
func ParseDriveURL(rawURL string) (DriveURLKind, string, error) {
	if m := reDriveFolder.FindStringSubmatch(rawURL); m != nil {
		return DriveKindFolder, m[1], nil
	}
	if m := reDriveFileD.FindStringSubmatch(rawURL); m != nil {
		return DriveKindFile, m[1], nil
	}
	if m := reDriveIDParam.FindStringSubmatch(rawURL); m != nil {
		return DriveKindFile, m[1], nil
	}
	return 0, "", fmt.Errorf("unrecognised Google Drive URL %q — expected a folder or file share link", rawURL)
}

// driveFilesPage is the JSON envelope from Drive API files.list.
type driveFilesPage struct {
	Files         []driveFileMeta `json:"files"`
	NextPageToken string          `json:"nextPageToken"`
}

// driveFileMeta is the metadata Drive returns for a single file.
type driveFileMeta struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// The Drive API encodes file size as a decimal string, not a JSON number.
	Size         string `json:"size"`
	ModifiedTime string `json:"modifiedTime"`
}

// toArchive converts Drive file metadata into a DriveArchive, parsing the
// decimal-string size best-effort.
func (m driveFileMeta) toArchive() DriveArchive {
	var size int64 = -1
	fmt.Sscanf(m.Size, "%d", &size) //nolint:errcheck // size is best-effort
	return DriveArchive{ID: m.ID, Name: m.Name, Size: size, Modified: m.ModifiedTime}
}

// driveGET performs an authenticated GET against the Drive API.
func driveGET(ctx context.Context, client *http.Client, apiURL, accessToken string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	return client.Do(req)
}

// ListDriveArchives returns all .tar.gz files whose immediate parent is folderID.
// accessToken must be a valid OAuth2 access token with drive.readonly scope.
func ListDriveArchives(ctx context.Context, folderID, accessToken string) ([]DriveArchive, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	var all []DriveArchive
	pageToken := ""

	for {
		q := url.Values{}
		q.Set("q", fmt.Sprintf(
			"'%s' in parents and name contains '.tar.gz' and trashed = false",
			folderID,
		))
		q.Set("fields", "nextPageToken,files(id,name,size,modifiedTime)")
		q.Set("pageSize", "100")
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}

		resp, err := driveGET(ctx, client, driveAPIBase+"?"+q.Encode(), accessToken)
		if err != nil {
			return nil, fmt.Errorf("querying Drive API: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			resp.Body.Close()
			return nil, fmt.Errorf("Drive API returned HTTP %d: %s",
				resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var page driveFilesPage
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decoding Drive API response: %w", err)
		}
		resp.Body.Close()

		for _, f := range page.Files {
			all = append(all, f.toArchive())
		}

		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}

	return all, nil
}

// DownloadDriveArchive streams a Drive file to a temporary .tar.gz file on disk
// using io.Copy, so the archive is never fully buffered in RAM. The caller must
// invoke the returned cleanup function when done.
func DownloadDriveArchive(ctx context.Context, archive DriveArchive, accessToken string) (path string, cleanup func(), err error) {
	q := url.Values{}
	q.Set("alt", "media")
	apiURL := fmt.Sprintf("%s/%s?%s", driveAPIBase, archive.ID, q.Encode())

	httpClient := &http.Client{Timeout: 30 * time.Minute}
	resp, err := driveGET(ctx, httpClient, apiURL, accessToken)
	if err != nil {
		return "", func() {}, fmt.Errorf("downloading %q: %w", archive.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", func() {}, fmt.Errorf("Drive returned HTTP %d for %q: %s",
			resp.StatusCode, archive.Name, strings.TrimSpace(string(body)))
	}

	return streamToTempFile(resp.Body, archive.Name)
}

// GetDriveFileName fetches the filename of a single Drive file by ID.
// accessToken must be a valid OAuth2 access token with drive.readonly scope.
func GetDriveFileName(ctx context.Context, fileID, accessToken string) (string, error) {
	archive, err := GetDriveFile(ctx, fileID, accessToken)
	if err != nil {
		return "", err
	}
	return archive.Name, nil
}

// GetDriveFile fetches a single Drive file's metadata (name, size, modified) by
// ID. accessToken must be a valid OAuth2 access token with drive.readonly scope.
func GetDriveFile(ctx context.Context, fileID, accessToken string) (DriveArchive, error) {
	q := url.Values{}
	q.Set("fields", "id,name,size,modifiedTime")
	apiURL := fmt.Sprintf("%s/%s?%s", driveAPIBase, fileID, q.Encode())

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := driveGET(ctx, client, apiURL, accessToken)
	if err != nil {
		return DriveArchive{}, fmt.Errorf("fetching file metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return DriveArchive{}, fmt.Errorf("Drive API returned HTTP %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var meta driveFileMeta
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return DriveArchive{}, fmt.Errorf("decoding file metadata: %w", err)
	}
	if meta.ID == "" {
		meta.ID = fileID
	}
	return meta.toArchive(), nil
}

// driveAboutResponse is the JSON response from Drive API about.get.
type driveAboutResponse struct {
	User struct {
		EmailAddress string `json:"emailAddress"`
		DisplayName  string `json:"displayName"`
	} `json:"user"`
}

// GetDriveAccountEmail returns the email address of the authenticated account,
// best-effort. It uses the Drive about.get endpoint, which the drive.readonly
// scope already permits. Returns ("", nil) when the email is unavailable.
func GetDriveAccountEmail(ctx context.Context, accessToken string) (string, error) {
	q := url.Values{}
	q.Set("fields", "user(emailAddress,displayName)")
	apiURL := "https://www.googleapis.com/drive/v3/about?" + q.Encode()

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := driveGET(ctx, client, apiURL, accessToken)
	if err != nil {
		return "", fmt.Errorf("fetching account info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("Drive API returned HTTP %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var about driveAboutResponse
	if err := json.NewDecoder(resp.Body).Decode(&about); err != nil {
		return "", fmt.Errorf("decoding account info: %w", err)
	}
	return about.User.EmailAddress, nil
}

// streamToTempFile copies r into a new temporary .tar.gz file and returns the
// path and a cleanup function. Callers must invoke cleanup when done.
func streamToTempFile(r io.Reader, name string) (path string, cleanup func(), err error) {
	tmp, err := os.CreateTemp("", "rag-gdrive-*.tar.gz")
	if err != nil {
		return "", func() {}, fmt.Errorf("creating temporary file: %w", err)
	}
	removeTmp := func() { _ = os.Remove(tmp.Name()) }

	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		removeTmp()
		return "", func() {}, fmt.Errorf("writing %q to disk: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		removeTmp()
		return "", func() {}, fmt.Errorf("closing temporary file: %w", err)
	}
	return tmp.Name(), removeTmp, nil
}
