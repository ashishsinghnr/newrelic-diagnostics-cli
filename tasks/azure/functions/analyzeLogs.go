package functions

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	log "github.com/newrelic/newrelic-diagnostics-cli/logger"
	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
)

// azureFuncLogPaths are the default log directories inside an Azure Function App container.
var azureFuncLogPaths = []string{
	"/home/LogFiles/Application",
	"/home/LogFiles",
}

// azureFuncLogGlobs are the file patterns to scan within each log directory.
var azureFuncLogGlobs = []string{"*.txt", "*.log"}

// LogEntry represents a single matched log line with its source file and pattern.
type LogEntry struct {
	File    string
	Line    string
	Pattern string
}

// LogAnalysisResult holds the categorised log findings.
type LogAnalysisResult struct {
	NRErrors      []LogEntry
	NRWarnings    []LogEntry
	GeneralErrors []LogEntry
	FilesScanned  int
}

// nrErrorPatterns are case-insensitive regexps that indicate NR agent errors.
var nrErrorPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)newrelic.*error`),
	regexp.MustCompile(`(?i)newrelic.*fail`),
	regexp.MustCompile(`(?i)newrelic.*exception`),
	regexp.MustCompile(`(?i)failed to connect to collector`),
	regexp.MustCompile(`(?i)license key.*invalid`),
	regexp.MustCompile(`(?i)ModuleNotFoundError.*newrelic`),
}

// nrWarningPatterns are case-insensitive regexps that indicate NR agent warnings.
var nrWarningPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)newrelic.*warn`),
	regexp.MustCompile(`(?i)newrelic.*deprecated`),
	regexp.MustCompile(`(?i)transaction.*timeout`),
}

// generalErrorPatterns catch non-NR errors worth surfacing.
var generalErrorPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\[ERROR\]`),
	regexp.MustCompile(`(?i)Exception:`),
	regexp.MustCompile(`(?i)Traceback`),
	regexp.MustCompile(`(?i)OutOfMemoryException`),
}

// AzureFunctionsAnalyzeLogs scans local Azure Function App log files for
// New Relic agent error and warning patterns.
type AzureFunctionsAnalyzeLogs struct {
	// logPathGlober allows injection for testing without real filesystem.
	logPathGlober func(pattern string) ([]string, error)
	// lineScanner allows injection for testing file reading.
	lineScanner func(path string) ([]string, error)
}

// Identifier returns the task identifier.
func (t AzureFunctionsAnalyzeLogs) Identifier() tasks.Identifier {
	return tasks.IdentifierFromString("Azure/Functions/AnalyzeLogs")
}

// Explain returns the help text for this task.
func (t AzureFunctionsAnalyzeLogs) Explain() string {
	return "Scan Azure Function App log files for New Relic agent errors and warnings"
}

// Dependencies returns the upstream tasks this task depends on.
func (t AzureFunctionsAnalyzeLogs) Dependencies() []string {
	return []string{
		taskDetectFunctionApp,
	}
}

// Execute scans the default Azure Functions log directories for NR patterns.
func (t AzureFunctionsAnalyzeLogs) Execute(options tasks.Options, upstream map[string]tasks.Result) tasks.Result {
	if upstream[taskDetectFunctionApp].Status != tasks.Info {
		return tasks.Result{
			Status:  tasks.None,
			Summary: "Not running in an Azure Function App; this task did not run",
		}
	}

	glober := t.logPathGlober
	if glober == nil {
		glober = filepath.Glob
	}
	scanner := t.lineScanner
	if scanner == nil {
		scanner = readLines
	}

	logFiles := collectLogFiles(glober)
	if len(logFiles) == 0 {
		return tasks.Result{
			Status:  tasks.None,
			Summary: "No log files found at Azure Function App default log paths (/home/LogFiles/)",
		}
	}

	analysis := &LogAnalysisResult{}
	for _, f := range logFiles {
		lines, err := scanner(f)
		if err != nil {
			log.Debug("Azure/Functions/AnalyzeLogs: could not read " + f + ": " + err.Error())
			continue
		}
		analysis.FilesScanned++
		scanLines(f, lines, analysis)
	}

	return buildLogAnalysisResult(analysis)
}

func collectLogFiles(glober func(string) ([]string, error)) []string {
	var files []string
	for _, dir := range azureFuncLogPaths {
		for _, pattern := range azureFuncLogGlobs {
			matches, err := glober(filepath.Join(dir, pattern))
			if err != nil {
				continue
			}
			files = append(files, matches...)
		}
	}
	return files
}

func scanLines(filename string, lines []string, out *LogAnalysisResult) {
	for _, line := range lines {
		// NR errors take priority.
		if matched, pattern := matchAny(line, nrErrorPatterns); matched {
			out.NRErrors = append(out.NRErrors, LogEntry{File: filename, Line: truncate(line, 200), Pattern: pattern})
			continue
		}
		if matched, pattern := matchAny(line, nrWarningPatterns); matched {
			out.NRWarnings = append(out.NRWarnings, LogEntry{File: filename, Line: truncate(line, 200), Pattern: pattern})
			continue
		}
		if matched, _ := matchAny(line, generalErrorPatterns); matched {
			if len(out.GeneralErrors) < maxGeneralErrors {
				out.GeneralErrors = append(out.GeneralErrors, LogEntry{File: filename, Line: truncate(line, 200)})
			}
		}
	}
}

func matchAny(line string, patterns []*regexp.Regexp) (bool, string) {
	for _, p := range patterns {
		if p.MatchString(line) {
			return true, p.String()
		}
	}
	return false, ""
}

func buildLogAnalysisResult(a *LogAnalysisResult) tasks.Result {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Scanned %d log file(s)\n", a.FilesScanned))

	if len(a.NRErrors) > 0 {
		sb.WriteString(fmt.Sprintf("  [FAILURE] %d New Relic error(s) found:\n", len(a.NRErrors)))
		for _, e := range limitEntries(a.NRErrors, maxReportedEntries) {
			sb.WriteString(fmt.Sprintf("    %s: %s\n", filepath.Base(e.File), e.Line))
		}
	}
	if len(a.NRWarnings) > 0 {
		sb.WriteString(fmt.Sprintf("  [WARNING] %d New Relic warning(s) found:\n", len(a.NRWarnings)))
		for _, w := range limitEntries(a.NRWarnings, maxReportedEntries) {
			sb.WriteString(fmt.Sprintf("    %s: %s\n", filepath.Base(w.File), w.Line))
		}
	}
	if len(a.GeneralErrors) > 0 {
		sb.WriteString(fmt.Sprintf("  [INFO] %d general error(s) found in logs\n", len(a.GeneralErrors)))
	}
	if len(a.NRErrors) == 0 && len(a.NRWarnings) == 0 && len(a.GeneralErrors) == 0 {
		sb.WriteString("  No New Relic errors or warnings found")
	}

	summary := strings.TrimRight(sb.String(), "\n")

	if len(a.NRErrors) > 0 {
		return tasks.Result{
			Status:  tasks.Failure,
			Summary: summary,
			Payload: a,
			URL:     "https://docs.newrelic.com/docs/apm/agents/net-agent/troubleshooting/",
		}
	}
	if len(a.NRWarnings) > 0 {
		return tasks.Result{
			Status:  tasks.Warning,
			Summary: summary,
			Payload: a,
		}
	}
	return tasks.Result{
		Status:  tasks.Success,
		Summary: summary,
		Payload: a,
	}
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func limitEntries(entries []LogEntry, n int) []LogEntry {
	if len(entries) <= n {
		return entries
	}
	return entries[:n]
}
