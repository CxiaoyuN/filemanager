package filemanager

import (
	"encoding/json"
	"html/template"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/asdine/storm"
)

// RequestContext contains the needed information to make handlers work.
type RequestContext struct {
	*FileManager
	User *User
	File *file
	// On API handlers, Router is the APi handler we want.
	Router string
}

// serveHTTP is the main entry point of this HTML application.
func serveHTTP(c *RequestContext, w http.ResponseWriter, r *http.Request) (int, error) {
	// Checks if the URL contains the baseURL and strips it. Otherwise, it just
	// returns a 404 error because we're not supposed to be here!
	p := strings.TrimPrefix(r.URL.Path, c.BaseURL)

	if len(p) >= len(r.URL.Path) && c.BaseURL != "" {
		return http.StatusNotFound, nil
	}

	r.URL.Path = p

	// Check if this request is made to the service worker. If so,
	// pass it through a template to add the needed variables.
	if r.URL.Path == "/sw.js" {
		return renderFile(
			c, w,
			c.assets.MustString("sw.js"),
			"application/javascript",
		)
	}

	// Checks if this request is made to the static assets folder. If so, and
	// if it is a GET request, returns with the asset. Otherwise, returns
	// a status not implemented.
	if matchURL(r.URL.Path, "/static") {
		if r.Method != http.MethodGet {
			return http.StatusNotImplemented, nil
		}

		return staticHandler(c, w, r)
	}

	// Checks if this request is made to the API and directs to the
	// API handler if so.
	if matchURL(r.URL.Path, "/api") {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api")
		return apiHandler(c, w, r)
	}

	// If it is a request to the preview and a static website generator is
	// active, build the preview.
	if strings.HasPrefix(r.URL.Path, "/preview") && c.StaticGen != nil {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/preview")
		return c.StaticGen.Preview(c, w, r)
	}

	if strings.HasPrefix(r.URL.Path, "/share/") {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/share/")
		return sharePage(c, w, r)
	}

	// Any other request should show the index.html file.
	w.Header().Set("x-frame-options", "SAMEORIGIN")
	w.Header().Set("x-content-type", "nosniff")
	w.Header().Set("x-xss-protection", "1; mode=block")

	return renderFile(
		c, w,
		c.assets.MustString("index.html"),
		"text/html",
	)
}

// staticHandler handles the static assets path.
func staticHandler(c *RequestContext, w http.ResponseWriter, r *http.Request) (int, error) {
	if r.URL.Path != "/static/manifest.json" {
		http.FileServer(c.assets.HTTPBox()).ServeHTTP(w, r)
		return 0, nil
	}

	return renderFile(
		c, w,
		c.assets.MustString("static/manifest.json"),
		"application/json",
	)
}

// apiHandler is the main entry point for the /api endpoint.
func apiHandler(c *RequestContext, w http.ResponseWriter, r *http.Request) (int, error) {
	if r.URL.Path == "/auth/get" {
		return authHandler(c, w, r)
	}

	if r.URL.Path == "/auth/renew" {
		return renewAuthHandler(c, w, r)
	}

	valid, _ := validateAuth(c, r)
	if !valid {
		return http.StatusForbidden, nil
	}

	c.Router, r.URL.Path = splitURL(r.URL.Path)

	if !c.User.Allowed(r.URL.Path) {
		return http.StatusForbidden, nil
	}

	if c.StaticGen != nil {
		// If we are using the 'magic url' for the settings,
		// we should redirect the request for the acutual path.
		if r.URL.Path == "/settings" {
			r.URL.Path = c.StaticGen.SettingsPath()
		}

		// Executes the Static website generator hook.
		code, err := c.StaticGen.Hook(c, w, r)
		if code != 0 || err != nil {
			return code, err
		}
	}

	if c.Router == "checksum" || c.Router == "download" {
		var err error
		c.File, err = getInfo(r.URL, c.FileManager, c.User)
		if err != nil {
			return errorToHTTP(err, false), err
		}
	}

	var code int
	var err error

	switch c.Router {
	case "download":
		code, err = downloadHandler(c, w, r)
	case "checksum":
		code, err = checksumHandler(c, w, r)
	case "command":
		code, err = command(c, w, r)
	case "search":
		code, err = search(c, w, r)
	case "resource":
		code, err = resourceHandler(c, w, r)
	case "users":
		code, err = usersHandler(c, w, r)
	case "settings":
		code, err = settingsHandler(c, w, r)
	case "share":
		code, err = shareHandler(c, w, r)
	default:
		code = http.StatusNotFound
	}

	return code, err
}

// serveChecksum calculates the hash of a file. Supports MD5, SHA1, SHA256 and SHA512.
func checksumHandler(c *RequestContext, w http.ResponseWriter, r *http.Request) (int, error) {
	query := r.URL.Query().Get("algo")

	val, err := c.File.Checksum(query)
	if err == errInvalidOption {
		return http.StatusBadRequest, err
	} else if err != nil {
		return http.StatusInternalServerError, err
	}

	w.Write([]byte(val))
	return 0, nil
}

// splitURL splits the path and returns everything that stands
// before the first slash and everything that goes after.
func splitURL(path string) (string, string) {
	if path == "" {
		return "", ""
	}

	path = strings.TrimPrefix(path, "/")

	i := strings.Index(path, "/")
	if i == -1 {
		return "", path
	}

	return path[0:i], path[i:]
}

// renderFile renders a file using a template with some needed variables.
func renderFile(c *RequestContext, w http.ResponseWriter, file string, contentType string) (int, error) {
	tpl := template.Must(template.New("file").Parse(file))
	w.Header().Set("Content-Type", contentType+"; charset=utf-8")

	err := tpl.Execute(w, map[string]interface{}{
		"BaseURL":   c.RootURL(),
		"StaticGen": c.staticgen,
	})
	if err != nil {
		return http.StatusInternalServerError, err
	}

	return 0, nil
}

func sharePage(c *RequestContext, w http.ResponseWriter, r *http.Request) (int, error) {
	var s shareLink
	err := c.db.One("Hash", r.URL.Path, &s)
	if err == storm.ErrNotFound {
		return renderFile(
			c, w,
			c.assets.MustString("static/share/404.html"),
			"text/html",
		)
	}

	if err != nil {
		return http.StatusInternalServerError, err
	}

	if s.Expires && s.ExpireDate.Before(time.Now()) {
		c.db.DeleteStruct(&s)
		return renderFile(
			c, w,
			c.assets.MustString("static/share/404.html"),
			"text/html",
		)
	}

	r.URL.Path = s.Path

	info, err := os.Stat(s.Path)
	if err != nil {
		return errorToHTTP(err, false), err
	}

	c.File = &file{
		Path:    s.Path,
		Name:    info.Name(),
		ModTime: info.ModTime(),
		Mode:    info.Mode(),
		IsDir:   info.IsDir(),
		Size:    info.Size(),
	}

	dl := r.URL.Query().Get("dl")

	if dl == "" || dl == "0" {
		tpl := template.Must(template.New("file").Parse(c.assets.MustString("static/share/index.html")))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		err := tpl.Execute(w, map[string]interface{}{
			"BaseURL": c.RootURL(),
			"File":    c.File,
		})

		if err != nil {
			return http.StatusInternalServerError, err
		}
		return 0, nil
	}

	return downloadHandler(c, w, r)
}

// renderJSON prints the JSON version of data to the browser.
func renderJSON(w http.ResponseWriter, data interface{}) (int, error) {
	marsh, err := json.Marshal(data)
	if err != nil {
		return http.StatusInternalServerError, err
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if _, err := w.Write(marsh); err != nil {
		return http.StatusInternalServerError, err
	}

	return 0, nil
}

// matchURL checks if the first URL matches the second.
func matchURL(first, second string) bool {
	first = strings.ToLower(first)
	second = strings.ToLower(second)

	return strings.HasPrefix(first, second)
}

// errorToHTTP converts errors to HTTP Status Code.
func errorToHTTP(err error, gone bool) int {
	switch {
	case err == nil:
		return http.StatusOK
	case os.IsPermission(err):
		return http.StatusForbidden
	case os.IsNotExist(err):
		if !gone {
			return http.StatusNotFound
		}

		return http.StatusGone
	case os.IsExist(err):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
