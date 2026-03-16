// email_map.go - Parsing username=email mapping files for import overrides
package importpkg

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ParseEmailMap reads a username=email mapping file. Lines starting with # are
// comments, blank lines are ignored.
func ParseEmailMap(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open email map: %w", err)
	}
	defer f.Close()
	emails := map[string]string{}
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("email map line %d: expected username=email, got %q", lineNum, line)
		}
		username := strings.TrimSpace(parts[0])
		email := strings.TrimSpace(parts[1])
		if username == "" || email == "" {
			return nil, fmt.Errorf("email map line %d: empty username or email", lineNum)
		}
		emails[username] = email
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read email map: %w", err)
	}
	return emails, nil
}
