package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"text/template"
)

var re = regexp.MustCompile(`^\[(.+)\]\((.+)\)\. (.+)$`)

var defaultBody = `---

url: ""
start_time: ""
end_time: ""
categories:
- postmortem
company: ""
product: ""

---

{{ .Data }}
`

// randomHex generates a random hex string intended for postmortem filenames.
func randomHex(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return hex.EncodeToString(bytes), nil
}

// ExtractPostmortems reads the collection of postmortems
// and extracts each postmortem to a separate file.
func ExtractPostmortems(dir string) error {
	file, err := os.Open("./tmp/posts.md")
	if err != nil {
		return fmt.Errorf("error opening file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		body := defaultBody
		pm := Postmortem{Description: scanner.Text()}

		if re.Match(scanner.Bytes()) {
			body = `---

url: "{{ .URL }}"
start_time: ""
end_time: ""
categories:
- postmortem
company: "{{ .Company }}"
product: ""

---

{{ .Description }}
`
			matches := re.FindStringSubmatch(scanner.Text())
			pm = Postmortem{URL: matches[2], Company: matches[1], Description: matches[3]}
		}

		err = savePostmortem(pm, body, dir)
		if err != nil {
			return fmt.Errorf("error saving postmortem file: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}

// savePostmortem takes the in-memory representation of the postmortem file and
// stores it in a file.
func savePostmortem(pm Postmortem, body, dir string) error {
	var data bytes.Buffer

	fm := template.Must(template.New("newPostmortemTemplate").Parse(body))
	if err := fm.Execute(&data, pm); err != nil {
		return fmt.Errorf("error executing template: %w", err)
	}

	// Generate a random string to set as filename.
	fnm, err := randomHex(8)
	if err != nil {
		return fmt.Errorf("error generating hex string: %w", err)
	}

	// Write postmortem data from memory to file.
	err = ioutil.WriteFile(filepath.Join(dir, fnm+".md"), data.Bytes(), 0644)
	if err != nil {
		return fmt.Errorf("error writing file: %w", err)
	}

	return nil
}
