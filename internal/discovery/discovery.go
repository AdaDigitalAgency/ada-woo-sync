package discovery

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var scanDirs = []string{"/home", "/var/www"}
var apacheConfDirs = []string{
	"/etc/apache2/sites-enabled",
	"/etc/apache2/sites-available",
}

var documentRootRe = regexp.MustCompile(`(?i)^\s*DocumentRoot\s+["']?([^"'\s]+)["']?`)
var serverNameRe = regexp.MustCompile(`(?i)^\s*ServerName\s+(\S+)`)

// VHost represents a discovered Apache virtual host.
type VHost struct {
	ServerName   string
	DocumentRoot string
}

// Scan looks for WordPress installations via Apache vhost configs first,
// then falls back to filesystem scanning for wp-config.php files.
// Apache results are preferred because they give us both path and domain.
func Scan() []string {
	seen := make(map[string]bool)
	var roots []string

	// 1. Parse Apache vhost configs
	for _, vhost := range ScanApacheVHosts() {
		root := vhost.DocumentRoot
		if seen[root] {
			continue
		}
		// Verify it's actually a WordPress install
		if _, err := os.Stat(filepath.Join(root, "wp-config.php")); err != nil {
			continue
		}
		seen[root] = true
		roots = append(roots, root)
	}

	// 2. Filesystem scan as fallback
	for _, dir := range scanDirs {
		filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return filepath.SkipDir
			}
			if d.IsDir() {
				rel, _ := filepath.Rel(dir, path)
				if strings.Count(rel, string(filepath.Separator)) > 2 {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Name() == "wp-config.php" {
				root := filepath.Dir(path)
				if !seen[root] {
					seen[root] = true
					roots = append(roots, root)
				}
			}
			return nil
		})
	}
	return roots
}

// ScanApacheVHosts parses Apache2 site configs and returns all vhosts
// with a DocumentRoot directive.
func ScanApacheVHosts() []VHost {
	var vhosts []VHost
	for _, dir := range apacheConfDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			parsed := parseApacheConf(path)
			vhosts = append(vhosts, parsed...)
		}
	}
	return vhosts
}

// parseApacheConf extracts VHost entries from a single Apache config file.
// Handles multiple <VirtualHost> blocks per file.
func parseApacheConf(path string) []VHost {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var vhosts []VHost
	var current *VHost

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		if strings.Contains(strings.ToLower(line), "<virtualhost") {
			current = &VHost{}
			continue
		}
		if strings.Contains(strings.ToLower(line), "</virtualhost") {
			if current != nil && current.DocumentRoot != "" {
				vhosts = append(vhosts, *current)
			}
			current = nil
			continue
		}
		if current == nil {
			continue
		}

		if m := documentRootRe.FindStringSubmatch(line); len(m) > 1 {
			current.DocumentRoot = m[1]
		}
		if m := serverNameRe.FindStringSubmatch(line); len(m) > 1 {
			current.ServerName = m[1]
		}
	}
	return vhosts
}

// ExtractDomain extracts the domain from a webroot path.
// /home/example.com/ → example.com
// /home/stage.example.com/ → stage.example.com
func ExtractDomain(webroot string) string {
	webroot = strings.TrimRight(webroot, "/")
	return filepath.Base(webroot)
}

// SitePair is an auto-detected live+stage pair.
type SitePair struct {
	LivePath  string
	StagePath string
	Domain    string // base domain without "stage." prefix
}

// PairSites groups discovered paths into live/stage pairs.
// Convention: "stage.example.com" is the staging counterpart of "example.com".
func PairSites(paths []string) []SitePair {
	byDomain := make(map[string]string) // domain → path
	for _, p := range paths {
		d := ExtractDomain(p)
		byDomain[d] = p
	}

	seen := make(map[string]bool)
	var pairs []SitePair

	for domain, path := range byDomain {
		if strings.HasPrefix(domain, "stage.") {
			continue // handled from the live side
		}
		stageDomain := "stage." + domain
		if stagePath, ok := byDomain[stageDomain]; ok {
			pairs = append(pairs, SitePair{
				LivePath:  path,
				StagePath: stagePath,
				Domain:    domain,
			})
			seen[domain] = true
			seen[stageDomain] = true
		}
	}

	// Include unpaired paths as live-only entries
	for domain, path := range byDomain {
		if !seen[domain] {
			pairs = append(pairs, SitePair{
				LivePath: path,
				Domain:   domain,
			})
		}
	}

	return pairs
}
