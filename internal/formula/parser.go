package formula

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// bottleBlockRegex matches the bottle do...end block
	bottleBlockRegex = regexp.MustCompile(`(?s)(\s*)bottle do\s*\n(.*?)\n\s*end`)
)

// ParseBottleBlock finds the bottle block in a formula and returns its start and end positions
// Returns the start position (index of 'b' in 'bottle'), end position (after 'end'), and error
func ParseBottleBlock(content string) (start, end int, err error) {
	matches := bottleBlockRegex.FindStringSubmatchIndex(content)
	if matches == nil {
		return 0, 0, fmt.Errorf("bottle block not found")
	}

	// matches[0] is the start of the entire match (including leading whitespace)
	// matches[1] is the end of the entire match
	// We want the actual 'bottle' keyword start, which is after the leading whitespace
	// matches[2] is start of first capture group (whitespace)
	// matches[3] is end of first capture group (whitespace)

	start = matches[2] // Start of leading whitespace
	end = matches[1]   // End of entire match

	return start, end, nil
}

// UpdateBottleBlock replaces the bottle block in the formula with a new one
func UpdateBottleBlock(content string, newBlock string) (string, error) {
	start, end, err := ParseBottleBlock(content)
	if err != nil {
		return "", err
	}

	// Ensure newBlock has proper formatting
	newBlock = strings.TrimSpace(newBlock)

	// Get the indentation from the original bottle block
	lines := strings.Split(content[:start+20], "\n") // Get lines up to bottle block
	lastLine := lines[len(lines)-1]
	indent := ""
	for _, r := range lastLine {
		if r == ' ' || r == '\t' {
			indent += string(r)
		} else {
			break
		}
	}

	// Add indentation to new block
	newBlockLines := strings.Split(newBlock, "\n")
	for i, line := range newBlockLines {
		if i == 0 || strings.TrimSpace(line) == "" {
			newBlockLines[i] = indent + line
		} else {
			newBlockLines[i] = indent + line
		}
	}
	newBlock = strings.Join(newBlockLines, "\n")

	// Replace the bottle block
	result := content[:start] + newBlock + content[end:]
	return result, nil
}

// GenerateBottleBlock generates a bottle do...end block from bottle specs
func GenerateBottleBlock(bottles []BottleSpec) string {
	if len(bottles) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("bottle do\n")
	sb.WriteString(fmt.Sprintf("  root_url \"%s\"\n", bottles[0].RootURL))

	for _, bottle := range bottles {
		sb.WriteString(fmt.Sprintf("  sha256 cellar: %s, %s: \"%s\"\n",
			bottle.Cellar, bottle.Platform, bottle.SHA256))
	}

	sb.WriteString("end")
	return sb.String()
}

// InsertBottleBlock inserts a bottle block after the license line in a formula
// This is useful when creating a new formula or when a bottle block doesn't exist
func InsertBottleBlock(content string, bottleBlock string) (string, error) {
	// Find the license line
	licenseRegex := regexp.MustCompile(`(?m)^\s*license\s+"[^"]*"\s*$`)
	match := licenseRegex.FindStringIndex(content)
	if match == nil {
		return "", fmt.Errorf("license line not found in formula")
	}

	// Insert after the license line
	insertPos := match[1]

	// Ensure we're inserting after the newline
	if insertPos < len(content) && content[insertPos] != '\n' {
		// Find the next newline
		nextNewline := strings.Index(content[insertPos:], "\n")
		if nextNewline >= 0 {
			insertPos += nextNewline + 1
		}
	} else {
		insertPos++ // Skip the newline
	}

	// Get indentation (should be 2 spaces for bottle block)
	indent := "  "

	// Format the bottle block with proper indentation
	bottleBlock = strings.TrimSpace(bottleBlock)
	lines := strings.Split(bottleBlock, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			lines[i] = indent + line
		}
	}
	formattedBlock := "\n" + strings.Join(lines, "\n") + "\n"

	// Insert the bottle block
	result := content[:insertPos] + formattedBlock + content[insertPos:]
	return result, nil
}
