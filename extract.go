package postmortems

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"regexp"

	guuid "github.com/google/uuid"
)

var (
	re       = regexp.MustCompile(`^\[(.+?)\]\((.+?)\)\. (.+)$`)
	bodyTmpl = `---
{{ yaml . }}
---

{{ .Description }}
`
)

// ExtractPostmortems reads the collection of postmortems
// and extracts each postmortem to a separate file.
func ExtractPostmortems(loc string, dir string) error {
	posts, err := ValidateDir(dir)
	if err != nil {
		return err
	}

	var data []byte
	if isURL(loc) {
		resp, err := http.Get(loc)
		if err != nil {
			return fmt.Errorf("could not get %q: %w", loc, err)
		}
		defer resp.Body.Close()

		data, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("could not read response body: %w", err)
		}
	} else if isFile(loc) {
		data, err = ioutil.ReadFile(loc)
		if err != nil {
			return fmt.Errorf("error opening file %q: %w", loc, err)
		}
	} else {
		return fmt.Errorf("%q is not a file or a url", loc)
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		id := guuid.New()
		pm := &Postmortem{UUID: id.String()}

		if re.Match(scanner.Bytes()) {
			matches := re.FindStringSubmatch(scanner.Text())
			pm.UUID = id.String()
			pm.URL = matches[2]
			pm.Company = matches[1]
			pm.Description = matches[3]
			pm.Categories = []string{"postmortem"}
		}

		// See if there is an existing one.
		for _, existing := range posts {
			if existing.URL == pm.URL {
				pm.UUID = existing.UUID
				pm.Categories = existing.Categories
				pm.Product = existing.Product
			}
		}

		if pm.URL != "" && pm.Company != "" && pm.Description != "" {
			if err := pm.Save(dir); err != nil {
				return fmt.Errorf("error saving postmortem file: %w", err)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}

func isURL(tgt string) bool {
	u, err := url.Parse(tgt)
	if err != nil {
		return false
	}

	return u.IsAbs() && u.Hostname() != ""
}

func isFile(tgt string) bool {
	info, err := os.Stat(tgt)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}
