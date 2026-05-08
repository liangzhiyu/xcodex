package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strconv"
	"strings"

	codex "github.com/liangzhiyu/xcodex/internal/xcodex"
)

var (
	version            = "dev"
	readBuildInfo      = debug.ReadBuildInfo
	findLatestSession  = codex.FindLatestSession
	findSessionByID    = codex.FindSessionByID
	findSessionByIndex = codex.FindSessionByIndex
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	switch os.Args[1] {
	case "version", "--version", "-v":
		printVersion()
	case "compress":
		cmdCompress(os.Args[2:])
	case "list":
		cmdList(os.Args[2:])
	case "search":
		cmdSearch(os.Args[2:])
	case "stats":
		cmdStats(os.Args[2:])
	case "diff":
		cmdDiff(os.Args[2:])
	case "clean":
		cmdClean(os.Args[2:])
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprint(os.Stderr, `xcodex — Codex 数据管理工具

Usage:
  xcodex <command> [options]

Commands:
  version    显示版本号
  compress   压缩会话上下文（语义剪枝 + token 预算）
  list       列出会话
  search     全文搜索会话内容
  stats      使用统计
  diff       文件变更摘要
  clean      清理旧数据

Use "xcodex <command> --help" for more information.
`)
}

func currentVersion() string {
	if version != "" && version != "dev" {
		return version
	}
	if info, ok := readBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
	}
	if version == "" {
		return "dev"
	}
	return version
}

func printVersion() {
	fmt.Printf("xcodex %s\n", currentVersion())
}

func parseTokenBudget(s string) int {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "k") || strings.HasSuffix(s, "K") {
		s = s[:len(s)-1]
		val, err := strconv.Atoi(s)
		if err == nil {
			return val * 1000
		}
	}
	v, err := strconv.Atoi(s)
	if err == nil && v > 0 {
		return v
	}
	return 64000
}

func copyToClipboard(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func resolveSessionFile(fileArg string) (string, error) {
	if fileArg != "" {
		return fileArg, nil
	}
	filePath, err := findLatestSession()
	if err != nil || filePath == "" {
		return "", fmt.Errorf("no Codex sessions found. Use --file to specify a session file")
	}
	return filePath, nil
}

func resolveSelectableSessionFile(fileArg string, selector string, cwd string, limit int) (string, error) {
	if fileArg != "" {
		return fileArg, nil
	}
	if selector != "" {
		index, err := strconv.Atoi(selector)
		if err == nil {
			if index <= 0 {
				return "", fmt.Errorf("session index must be >= 1")
			}
			return findSessionByIndex(index, cwd, limit)
		}
		return findSessionByID(selector)
	}
	filePath, err := findLatestSession()
	if err != nil || filePath == "" {
		return "", fmt.Errorf("no Codex sessions found. Use --file to specify a session file")
	}
	return filePath, nil
}

func resolveCompressSessionFile(fileArg string, selector string, cwd string, limit int) (string, error) {
	return resolveSelectableSessionFile(fileArg, selector, cwd, limit)
}

func resolveDiffSessionFile(fileArg string, selector string, cwd string, limit int) (string, error) {
	return resolveSelectableSessionFile(fileArg, selector, cwd, limit)
}

// --- compress ---

func cmdCompress(args []string) {
	fs := newFlagSet("compress")
	file := fs.String("file", "", "Path to specific session JSONL file")
	cwd := fs.String("cwd", "", "Filter by project path when selecting by index")
	limit := fs.Int("limit", 15, "Session window used for index lookup")
	tokens := fs.String("tokens", "64k", "Max token budget (e.g. 64k, 32k)")
	copy := fs.Bool("copy", false, "Copy output to clipboard")
	fs.Parse(args)

	remaining := fs.Args()
	if len(remaining) > 1 {
		fmt.Fprintln(os.Stderr, "Error: compress accepts at most one session selector")
		os.Exit(1)
	}

	selector := ""
	if len(remaining) == 1 {
		selector = remaining[0]
	}

	maxTokens := parseTokenBudget(*tokens)

	filePath, err := resolveCompressSessionFile(*file, selector, *cwd, *limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	session, err := codex.ParseSession(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing session: %v\n", err)
		os.Exit(1)
	}

	output := codex.GenerateCompress(session, maxTokens)

	if *copy {
		if err := copyToClipboard(output); err != nil {
			fmt.Println(output)
			fmt.Println("\n(Could not copy to clipboard, output printed above)")
		} else {
			fmt.Println("Summary copied to clipboard.")
		}
	} else {
		fmt.Println(output)
	}
}

// --- list ---

func cmdList(args []string) {
	fs := newFlagSet("list")
	limit := fs.Int("limit", 15, "Number of sessions to list")
	cwd := fs.String("cwd", "", "Filter by project path")
	fs.Parse(args)

	var sessions []codex.SessionEntry
	var err error
	if *cwd != "" {
		sessions, err = codex.ListSessionsByCwd(*cwd, *limit)
	} else {
		sessions, err = codex.ListRecentSessions(*limit)
	}

	if err != nil || len(sessions) == 0 {
		fmt.Println("No Codex sessions found.")
		return
	}

	fmt.Println("Recent Codex sessions:")
	fmt.Println()
	for i, s := range sessions {
		tokenStr := ""
		if s.TokensUsed > 0 {
			tokenStr = fmt.Sprintf(" | tokens: %s", formatNumber(s.TokensUsed))
		}
		fmt.Printf("  %d. [%s] %s%s\n", i+1, s.UpdatedAt, s.Title, tokenStr)
		fmt.Printf("     cwd: %s\n", s.Cwd)
		fmt.Printf("     file: %s\n\n", s.RolloutPath)
	}
}

// --- search ---

func cmdSearch(args []string) {
	fs := newFlagSet("search")
	cwd := fs.String("cwd", "", "Filter by project path")
	after := fs.String("after", "", "Only sessions within period (e.g. 7d, 30d)")
	fs.Parse(args)

	remaining := fs.Args()
	if len(remaining) == 0 {
		fmt.Fprintln(os.Stderr, "Error: search keyword required")
		fmt.Fprintln(os.Stderr, "Usage: xcodex search <keyword> [--cwd path] [--after 7d]")
		os.Exit(1)
	}
	keyword := remaining[0]

	sinceDays := parseDuration(*after)
	results, err := codex.SearchSessions(keyword, *cwd, sinceDays)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(results) == 0 {
		fmt.Println("No matches found.")
		return
	}

	for _, r := range results {
		fmt.Printf("[%s] %s\n", r.UpdatedAt, r.Title)
		fmt.Printf("  cwd: %s\n", r.Cwd)
		for _, m := range r.Matches {
			fmt.Printf("  → %s\n", m)
		}
		fmt.Println()
	}
}

// --- stats ---

func cmdStats(args []string) {
	fs := newFlagSet("stats")
	by := fs.String("by", "", "Group by: project, model, day")
	since := fs.String("since", "", "Period (e.g. 30d, 7d)")
	fs.Parse(args)

	sinceDays := parseDuration(*since)
	result, err := codex.AnalyzeStats(sinceDays)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("总会话: %d | 总 token: %s | 项目数: %d\n\n", result.TotalSessions, formatNumber(result.TotalTokens), result.ProjectCount)

	switch *by {
	case "project":
		for _, g := range result.ByProject {
			fmt.Printf("  %-40s  %d 会话  %s tokens\n", g.Key, g.Count, formatNumber(g.Tokens))
		}
	case "model":
		for _, g := range result.ByModel {
			fmt.Printf("  %-20s  %d 会话  %s tokens\n", g.Key, g.Count, formatNumber(g.Tokens))
		}
	case "day":
		for _, g := range result.ByDay {
			fmt.Printf("  %-12s  %d 会话  %s tokens\n", g.Key, g.Count, formatNumber(g.Tokens))
		}
	default:
		if len(result.ByProject) > 0 {
			fmt.Println("按项目:")
			for _, g := range result.ByProject {
				fmt.Printf("  %-40s  %d 会话  %s tokens\n", g.Key, g.Count, formatNumber(g.Tokens))
			}
			fmt.Println()
		}
		if len(result.ByModel) > 0 {
			fmt.Println("按模型:")
			for _, g := range result.ByModel {
				fmt.Printf("  %-20s  %d 会话  %s tokens\n", g.Key, g.Count, formatNumber(g.Tokens))
			}
		}
	}
}

// --- diff ---

func cmdDiff(args []string) {
	fs := newFlagSet("diff")
	file := fs.String("file", "", "Path to specific session JSONL file")
	cwd := fs.String("cwd", "", "Filter by project path when selecting by index")
	limit := fs.Int("limit", 15, "Session window used for index lookup")
	verbose := fs.Bool("verbose", false, "Show change content summary")
	fs.Parse(args)

	remaining := fs.Args()
	if len(remaining) > 1 {
		fmt.Fprintln(os.Stderr, "Error: diff accepts at most one session selector")
		os.Exit(1)
	}

	selector := ""
	if len(remaining) == 1 {
		selector = remaining[0]
	}

	filePath, err := resolveDiffSessionFile(*file, selector, *cwd, *limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	session, err := codex.ParseSession(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing session: %v\n", err)
		os.Exit(1)
	}

	changes := codex.ExtractDiff(session, *verbose)
	if len(changes) == 0 {
		fmt.Println("No file changes found.")
		return
	}

	for _, c := range changes {
		fmt.Printf("%s %s", c.Action, c.Path)
		if c.Summary != "" {
			fmt.Printf("  (%s)", c.Summary)
		}
		fmt.Println()
	}
}

// --- clean ---

func cmdClean(args []string) {
	fs := newFlagSet("clean")
	dryRun := fs.Bool("dry-run", false, "Preview files to clean without deleting")
	olderThan := fs.String("older-than", "30d", "Clean files older than this period")
	archivedOnly := fs.Bool("archived-only", false, "Only clean archived sessions")
	fs.Parse(args)

	days := parseDuration(*olderThan)
	if days <= 0 {
		days = 30
	}

	result, err := codex.CleanOldSessions(codex.CleanOptions{
		DryRun:        *dryRun,
		OlderThanDays: days,
		ArchivedOnly:  *archivedOnly,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if *dryRun {
		fmt.Printf("Would clean %d files (%s)\n\n", len(result.Files), formatSize(result.TotalSize))
	} else {
		fmt.Printf("Cleaned %d files (%s)\n\n", len(result.Files), formatSize(result.TotalSize))
	}

	for _, f := range result.Files {
		fmt.Printf("  %s  %s\n", formatSize(f.Size), f.Path)
	}
}

// --- flag parsing ---

func newFlagSet(name string) *flagSet {
	return &flagSet{name: name}
}

type flagSet struct {
	name  string
	flags []flagEntry
	args  []string
}

type flagEntry struct {
	name   string
	value  interface{}
	isBool bool
}

func (fs *flagSet) String(name, def, usage string) *string {
	p := def
	fs.flags = append(fs.flags, flagEntry{name: name, value: &p})
	return &p
}

func (fs *flagSet) Int(name string, def int, usage string) *int {
	p := def
	fs.flags = append(fs.flags, flagEntry{name: name, value: &p})
	return &p
}

func (fs *flagSet) Bool(name string, def bool, usage string) *bool {
	p := def
	fs.flags = append(fs.flags, flagEntry{name: name, value: &p, isBool: true})
	return &p
}

func (fs *flagSet) Parse(args []string) {
	i := 0
	for i < len(args) {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			fs.args = append(fs.args, arg)
			i++
			continue
		}
		name := strings.TrimLeft(arg, "-")
		for _, f := range fs.flags {
			if f.name == name {
				if f.isBool {
					*(f.value.(*bool)) = true
				} else if i+1 < len(args) {
					i++
					switch v := f.value.(type) {
					case *string:
						*v = args[i]
					case *int:
						*v, _ = strconv.Atoi(args[i])
					}
				}
				break
			}
		}
		i++
	}
}

func (fs *flagSet) Args() []string {
	return fs.args
}

// --- helpers ---

func parseDuration(s string) int {
	if s == "" {
		return 0
	}
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		d, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err == nil {
			return d
		}
	}
	d, err := strconv.Atoi(s)
	if err == nil {
		return d
	}
	return 0
}

func formatNumber(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var parts []string
	for i := len(s); i > 0; i -= 3 {
		start := i - 3
		if start < 0 {
			start = 0
		}
		parts = append([]string{s[start:i]}, parts...)
	}
	return strings.Join(parts, ",")
}

func formatSize(n int64) string {
	const kb = 1024
	const mb = kb * 1024
	if n >= mb {
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	}
	if n >= kb {
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	}
	return fmt.Sprintf("%d B", n)
}
