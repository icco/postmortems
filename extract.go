package postmortems

import (
	"bufio"
	"fmt"
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
func ExtractPostmortems(dir string) error {
	posts, err := ValidateDir(dir)
	if err != nil {
		return err
	}

	file, err := os.Open("./tmp/posts.md")
	if err != nil {
		return fmt.Errorf("error opening file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
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

		err = pm.Save(dir)
		if err != nil {
			return fmt.Errorf("error saving postmortem file: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}
