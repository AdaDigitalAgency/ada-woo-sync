package wpconfig

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

type WPConfig struct {
	DBName      string
	DBUser      string
	DBPassword  string
	DBHost      string
	TablePrefix string
}

var defineRe = regexp.MustCompile(`define\s*\(\s*['"](\w+)['"]\s*,\s*['"]([^'"]*)['"]\s*\)`)
var prefixRe = regexp.MustCompile(`\$table_prefix\s*=\s*['"]([^'"]*)['"]\s*;`)

func Parse(path string) (*WPConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	content := string(data)

	cfg := &WPConfig{
		TablePrefix: "wp_", // default
	}

	defines := make(map[string]string)
	for _, match := range defineRe.FindAllStringSubmatch(content, -1) {
		defines[match[1]] = match[2]
	}

	required := map[string]*string{
		"DB_NAME":     &cfg.DBName,
		"DB_USER":     &cfg.DBUser,
		"DB_PASSWORD": &cfg.DBPassword,
		"DB_HOST":     &cfg.DBHost,
	}
	var missing []string
	for key, target := range required {
		val, ok := defines[key]
		if !ok {
			missing = append(missing, key)
			continue
		}
		*target = val
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing defines in %s: %s", path, strings.Join(missing, ", "))
	}

	if m := prefixRe.FindStringSubmatch(content); len(m) > 1 {
		cfg.TablePrefix = m[1]
	}

	return cfg, nil
}
