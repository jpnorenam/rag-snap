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

	"golang.org/x/net/html"
)

const driveAPIBase = "https://www.googleapis.com/drive/v3/files"

// DriveArchive describes a .tar.gz file discovered in a Google Drive folder.
type DriveArchive struct {
	ID   string
	Name string
	// Size is the file size in bytes as reported by the Drive API, or -1 when unavailable.
	Size int64
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
	Files []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		// The Drive API encodes file size as a decimal string, not a JSON number.
		Size string `json:"size"`
	} `json:"files"`
	NextPageToken string `json:"nextPageToken"`
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
		q.Set("fields", "nextPageToken,files(id,name,size)")
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
			var size int64 = -1
			fmt.Sscanf(f.Size, "%d", &size) //nolint:errcheck // size is best-effort
			all = append(all, DriveArchive{ID: f.ID, Name: f.Name, Size: size})
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

// ListPublicDriveArchives lists .tar.gz files in a publicly shared Drive folder
// without authentication by parsing the embedded folder view HTML that Google
// renders for "anyone with the link" folders.
//
// This is a best-effort scraping approach for testing only. Production use
// should call ListDriveArchives with an OAuth access token instead.
func ListPublicDriveArchives(ctx context.Context, folderID string) ([]DriveArchive, error) {
	viewURL := "https://drive.google.com/embeddedfolderview?id=" + folderID + "#list"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, viewURL, nil) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("building folder view request: %w", err)
	}
	// A browser User-Agent is required; Google returns a redirect to the sign-in
	// page for requests that look like bots.
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching folder view: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("folder view returned HTTP %d — the folder may not be publicly shared", resp.StatusCode)
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parsing folder HTML: %w", err)
	}

	var archives []DriveArchive
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "div" {
			// Google renders each file as <div id="entry-FILE_ID" ...>
			// with a descendant element carrying title="filename.tar.gz".
			var fileID string
			for _, a := range n.Attr {
				if a.Key == "id" && strings.HasPrefix(a.Val, "entry-") {
					fileID = strings.TrimPrefix(a.Val, "entry-")
					break
				}
			}
			if fileID != "" {
				if name := findTarGzTitle(n); name != "" {
					archives = append(archives, DriveArchive{ID: fileID, Name: name, Size: -1})
				}
				return // don't descend into the entry's subtree again
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return archives, nil
}

// findTarGzTitle does a depth-first search of the HTML subtree rooted at n,
// returning the value of the first title= attribute that ends in ".tar.gz".
func findTarGzTitle(n *html.Node) string {
	if n.Type == html.ElementNode {
		for _, a := range n.Attr {
			if a.Key == "title" && strings.HasSuffix(strings.ToLower(a.Val), ".tar.gz") {
				return a.Val
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if name := findTarGzTitle(c); name != "" {
			return name
		}
	}
	return ""
}

// DownloadPublicDriveArchive downloads a publicly shared Drive file without
// authentication. Use this only when the file is shared as "anyone with the
// link can view". It uses the drive.usercontent.google.com domain with
// confirm=t, which bypasses the large-file virus-scan warning page without
// requiring HTML parsing.
//
// The second return value is the resolved filename extracted from the
// Content-Disposition header. Callers should use it instead of archive.Name
// when archive.Name is a URL (single-file import case).
func DownloadPublicDriveArchive(ctx context.Context, archive DriveArchive) (path, filename string, cleanup func(), err error) {
	// drive.usercontent.google.com is Google's current CDN for Drive downloads.
	// confirm=t pre-acknowledges the virus-scan warning for large files.
	dlURL := fmt.Sprintf(
		"https://drive.usercontent.google.com/download?id=%s&export=download&confirm=t",
		archive.ID,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil) //nolint:gosec
	if err != nil {
		return "", "", func() {}, fmt.Errorf("building download request: %w", err)
	}

	httpClient := &http.Client{Timeout: 30 * time.Minute}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", func() {}, fmt.Errorf("downloading %q: %w", archive.ID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", "", func() {}, fmt.Errorf("Drive returned HTTP %d for file %q: %s",
			resp.StatusCode, archive.ID, strings.TrimSpace(string(body)))
	}

	// Guard against receiving an HTML error page (e.g. a login redirect).
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "text/html") {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", "", func() {}, fmt.Errorf(
			"Drive returned an HTML page instead of a file — the file may not be publicly shared\n%s",
			strings.TrimSpace(string(body)),
		)
	}

	// Resolve the actual filename from Content-Disposition so callers can
	// derive a sensible KB name rather than using the raw URL.
	resolved := filenameFromContentDisposition(resp.Header.Get("Content-Disposition"))
	if resolved == "" {
		resolved = archive.ID + ".tar.gz"
	}

	path, cleanup, err = streamToTempFile(resp.Body, resolved)
	return path, resolved, cleanup, err
}

// filenameFromContentDisposition extracts the filename value from a
// Content-Disposition header such as `attachment; filename="foo.tar.gz"`.
func filenameFromContentDisposition(cd string) string {
	for _, part := range strings.Split(cd, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "filename=") {
			name := strings.TrimPrefix(part, "filename=")
			name = strings.Trim(name, `"'`)
			return name
		}
	}
	return ""
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
