package service

import (
	"fmt"
	"os"
	"strings"
)

// plistHeader and plistFooter wrap the dict of every launchd job.
const (
	plistHeader = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`
	plistFooter = `</dict>
</plist>
`
)

// xmlText escapes the three characters that cannot appear literally in
// element text. Paths routinely contain an ampersand, so this is not
// decoration.
var xmlText = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// esc escapes s for use inside an XML element.
func esc(s string) string { return xmlText.Replace(s) }

// Plist renders the launchd property list for this configuration.
//
// The serve job runs at load, is kept alive whenever it exits unsuccessfully
// and is marked Background so the scheduler treats it as a daemon. The
// archive job does not run at load; it runs once a day at Hour:Minute and is
// never restarted, because a job that exits after doing its work is done.
func (c Config) Plist() (string, error) {
	argv, err := c.Args()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(plistHeader)
	writeKeyString(&b, "Label", c.Label)
	writeKeyArray(&b, "ProgramArguments", argv)

	switch c.Kind {
	case KindServe:
		writeKeyBool(&b, "RunAtLoad", true)
		// A dict rather than <true/>: launchd then restarts the dashboard
		// after a crash but leaves it alone after a clean stop.
		b.WriteString("\t<key>KeepAlive</key>\n\t<dict>\n\t\t<key>SuccessfulExit</key>\n\t\t<false/>\n\t</dict>\n")
	case KindArchive:
		writeKeyBool(&b, "RunAtLoad", false)
		b.WriteString("\t<key>StartCalendarInterval</key>\n\t<dict>\n")
		fmt.Fprintf(&b, "\t\t<key>Hour</key>\n\t\t<integer>%d</integer>\n", c.Hour)
		fmt.Fprintf(&b, "\t\t<key>Minute</key>\n\t\t<integer>%d</integer>\n", c.Minute)
		b.WriteString("\t</dict>\n")
	}

	writeKeyString(&b, "StandardOutPath", c.StdoutPath())
	writeKeyString(&b, "StandardErrorPath", c.StderrPath())
	writeKeyString(&b, "ProcessType", "Background")
	b.WriteString(plistFooter)
	return b.String(), nil
}

func writeKeyString(b *strings.Builder, key, value string) {
	fmt.Fprintf(b, "\t<key>%s</key>\n\t<string>%s</string>\n", esc(key), esc(value))
}

func writeKeyBool(b *strings.Builder, key string, value bool) {
	tag := "false"
	if value {
		tag = "true"
	}
	fmt.Fprintf(b, "\t<key>%s</key>\n\t<%s/>\n", esc(key), tag)
}

func writeKeyArray(b *strings.Builder, key string, values []string) {
	fmt.Fprintf(b, "\t<key>%s</key>\n\t<array>\n", esc(key))
	for _, v := range values {
		fmt.Fprintf(b, "\t\t<string>%s</string>\n", esc(v))
	}
	b.WriteString("\t</array>\n")
}

// InstalledAddr reads the --addr value out of an installed serve plist, so
// 'recall service url' prints the URL the running dashboard actually
// answers on rather than the compiled-in default.
func InstalledAddr(c Config) (string, bool) {
	data, err := os.ReadFile(c.PlistPath())
	if err != nil {
		return "", false
	}
	return addrFromPlist(string(data))
}

// addrFromPlist finds the <string> that follows the --addr argument.
func addrFromPlist(s string) (string, bool) {
	const flag = "<string>--addr</string>"
	i := strings.Index(s, flag)
	if i < 0 {
		return "", false
	}
	rest := s[i+len(flag):]
	open := strings.Index(rest, "<string>")
	if open < 0 {
		return "", false
	}
	rest = rest[open+len("<string>"):]
	end := strings.Index(rest, "</string>")
	if end < 0 {
		return "", false
	}
	addr := strings.TrimSpace(rest[:end])
	if addr == "" {
		return "", false
	}
	return strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">").Replace(addr), true
}
