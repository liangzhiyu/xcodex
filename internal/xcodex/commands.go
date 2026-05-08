package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// --- shared helpers ---

var fileEditTools = map[string]bool{"apply_patch": true, "write_file": true, "create_file": true}
var skipTools = map[string]bool{"write_stdin": true}

func isValidProjectPath(p, cwd string) bool {
	if len(p) < 2 {
		return false
	}
	if strings.HasPrefix(p, "/dev/") || strings.HasPrefix(p, "/tmp/") ||
		strings.HasPrefix(p, "/var/") || strings.HasPrefix(p, "/etc/") ||
		strings.HasPrefix(p, "/home/") || strings.HasPrefix(p, "/proc/") ||
		strings.HasPrefix(p, "/sys/") {
		return false
	}
	if strings.HasPrefix(p, "$") || strings.HasPrefix(p, ">") {
		return false
	}
	if cwd != "" && strings.HasPrefix(p, "/") {
		return strings.HasPrefix(p, strings.TrimRight(cwd, "/"))
	}
	return true
}

func extractFilePathFromArgs(args string) string {
	var m map[string]any
	if json.Unmarshal([]byte(args), &m) == nil {
		for _, key := range []string{"path", "file_path", "filePath", "cmd"} {
			if v, ok := m[key]; ok {
				if s, ok := v.(string); ok {
					return s
				}
			}
		}
	}
	if len(args) > 100 {
		return args[:100]
	}
	return args
}

func extractFilePathFromToolCall(tc ToolCall) string {
	return extractFilePathFromArgs(tc.Args)
}

func detectFileEditsFromCommand(cmd string) []struct{ Path, Action string } {
	var edits []struct{ Path, Action string }

	if m := regexp.MustCompile(`sed\s+(?:-i(?:''|""|\s)|--in-place)\s+.*?\s+['"]?([^\s'"]+)['"]?\s*$`).FindStringSubmatch(cmd); len(m) > 1 {
		edits = append(edits, struct{ Path, Action string }{Path: m[1], Action: "修改"})
	}
	if m := regexp.MustCompile(`cat\s*>\s+['"]?([^\s'";|&]+)['"]?`).FindStringSubmatch(cmd); len(m) > 1 {
		edits = append(edits, struct{ Path, Action string }{Path: m[1], Action: "写入"})
	}
	if m := regexp.MustCompile(`\btee\s+['"]?([^\s'";|&]+)['"]?`).FindStringSubmatch(cmd); len(m) > 1 {
		edits = append(edits, struct{ Path, Action string }{Path: m[1], Action: "写入"})
	}
	if strings.Contains(cmd, "apply_patch") {
		if m := regexp.MustCompile(`apply_patch\s+.*?--file\s+['"]?([^\s'"]+)['"]?`).FindStringSubmatch(cmd); len(m) > 1 {
			edits = append(edits, struct{ Path, Action string }{Path: m[1], Action: "修改"})
		}
	}
	if m := regexp.MustCompile(`\s[>]{1,2}\s+['"]?([^\s'";|&]+)['"]?`).FindStringSubmatch(cmd); len(m) > 1 {
		p := m[1]
		if p != "/dev/null" && !strings.HasPrefix(p, ">") && !strings.HasPrefix(p, "&") && len(p) > 3 &&
			!strings.ContainsAny(p, "\\%") && strings.Contains(p, "/") {
			dup := false
			for _, e := range edits {
				if e.Path == p {
					dup = true
					break
				}
			}
			if !dup {
				edits = append(edits, struct{ Path, Action string }{Path: p, Action: "写入"})
			}
		}
	}
	return edits
}

// --- compress ---

func EstimateTokens(text string) int {
	tokens := 0
	for _, ch := range text {
		if ch > 0x7f {
			tokens += 2
		} else {
			tokens += 1
		}
	}
	return tokens * 3 / 4
}

func lastMeaningfulText(texts []string) string {
	for i := len(texts) - 1; i >= 0; i-- {
		if len(texts[i]) > 20 {
			return texts[i]
		}
	}
	return ""
}

func extractFileChanges(turns []Turn, cwd string) []string {
	seen := make(map[string]string)
	for _, turn := range turns {
		for _, tc := range turn.ToolCalls {
			if fileEditTools[tc.Name] {
				p := extractFilePathFromToolCall(tc)
				if isValidProjectPath(p, cwd) {
					if tc.Name == "write_file" || tc.Name == "create_file" {
						seen[p] = "新建"
					} else {
						seen[p] = "修改"
					}
				}
			}
			if tc.Name == "exec_command" {
				var args struct {
					Cmd string `json:"cmd"`
				}
				if json.Unmarshal([]byte(tc.Args), &args) == nil {
					for _, e := range detectFileEditsFromCommand(args.Cmd) {
						if isValidProjectPath(e.Path, cwd) {
							seen[e.Path] = e.Action
						}
					}
				}
			}
		}
	}
	var result []string
	for p, label := range seen {
		result = append(result, fmt.Sprintf("- `%s` — %s", p, label))
	}
	sort.Strings(result)
	return result
}

func extractDecisions(turns []Turn) []string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`选择(?:了|用)?\s*.+`),
		regexp.MustCompile(`决定(?:了)?\s*.+`),
		regexp.MustCompile(`采用\s*.+`),
		regexp.MustCompile(`使用\s*.+(?:方案|方式|策略|库|框架)`),
		regexp.MustCompile(`(?i)chose\s+to`),
		regexp.MustCompile(`(?i)decided\s+to`),
		regexp.MustCompile(`(?i)opted\s+(?:for|to)`),
	}

	seen := make(map[string]bool)
	var decisions []string
	for _, turn := range turns {
		text := lastMeaningfulText(turn.AssistantTexts)
		if text == "" {
			continue
		}
		for _, line := range strings.Split(text, "\n") {
			trimmed := strings.TrimSpace(line)
			if len(trimmed) < 10 || len(trimmed) > 200 {
				continue
			}
			for _, p := range patterns {
				if p.MatchString(trimmed) {
					cleaned := regexp.MustCompile(`^[-*]\s*`).ReplaceAllString(trimmed, "")
					if !seen[cleaned] {
						seen[cleaned] = true
						decisions = append(decisions, cleaned)
					}
					break
				}
			}
			if len(decisions) >= 10 {
				return decisions
			}
		}
	}
	return decisions
}

func extractPendingItems(turns []Turn) []string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:还需要|待办|TODO|接下来需要|remaining|still need|left to)\s*.+`),
		regexp.MustCompile(`(?i)(?:接下来|下一步|next step)\s*.+`),
	}

	start := len(turns) - 5
	if start < 0 {
		start = 0
	}

	seen := make(map[string]bool)
	var items []string
	for _, turn := range turns[start:] {
		text := lastMeaningfulText(turn.AssistantTexts)
		if text == "" {
			continue
		}
		for _, line := range strings.Split(text, "\n") {
			trimmed := strings.TrimSpace(line)
			if len(trimmed) < 5 || len(trimmed) > 200 {
				continue
			}
			for _, p := range patterns {
				if p.MatchString(trimmed) {
					cleaned := regexp.MustCompile(`^[-*]\s*`).ReplaceAllString(trimmed, "")
					if !seen[cleaned] {
						seen[cleaned] = true
						items = append(items, cleaned)
					}
					break
				}
			}
			if len(items) >= 10 {
				return items
			}
		}
	}
	return items
}

func extractWorkDone(turns []Turn, cwd string) []string {
	seen := make(map[string]bool)
	var items []string
	for _, turn := range turns {
		for _, tc := range turn.ToolCalls {
			if fileEditTools[tc.Name] {
				p := extractFilePathFromToolCall(tc)
				if isValidProjectPath(p, cwd) {
					label := "修改"
					if tc.Name == "write_file" || tc.Name == "create_file" {
						label = "新建"
					}
					item := fmt.Sprintf("%s %s", label, p)
					if !seen[item] {
						seen[item] = true
						items = append(items, item)
					}
				}
			}
			if tc.Name == "exec_command" {
				var args struct {
					Cmd string `json:"cmd"`
				}
				if json.Unmarshal([]byte(tc.Args), &args) == nil {
					for _, e := range detectFileEditsFromCommand(args.Cmd) {
						if isValidProjectPath(e.Path, cwd) {
							item := fmt.Sprintf("%s %s", e.Action, e.Path)
							if !seen[item] {
								seen[item] = true
								items = append(items, item)
							}
						}
					}
				}
			}
			if len(items) >= 15 {
				break
			}
		}
	}
	return items
}

func describeToolCall(tc ToolCall) string {
	if skipTools[tc.Name] {
		return ""
	}
	switch tc.Name {
	case "exec_command":
		var args struct {
			Cmd string `json:"cmd"`
		}
		cmd := tc.Args
		if json.Unmarshal([]byte(tc.Args), &args) == nil {
			cmd = args.Cmd
		}
		edits := detectFileEditsFromCommand(cmd)
		if len(edits) > 0 {
			var parts []string
			for _, e := range edits {
				parts = append(parts, fmt.Sprintf("[Edit: %s]", e.Path))
			}
			return strings.Join(parts, ", ")
		}
		readRe := regexp.MustCompile(`^(cat|head|tail|sed -n|less|more|rg |grep |find |ls |pwd|git (log|diff|status|show|branch)|wc )`)
		if readRe.MatchString(cmd) {
			if len(cmd) > 80 {
				cmd = cmd[:80]
			}
			return "[Read] " + cmd
		}
		if len(cmd) > 80 {
			cmd = cmd[:80]
		}
		return "[Run] " + cmd
	case "apply_patch":
		return "[Edit: " + extractFilePathFromToolCall(tc) + "]"
	case "write_file", "create_file":
		return "[Write: " + extractFilePathFromToolCall(tc) + "]"
	case "read_file":
		return "[Read: " + extractFilePathFromToolCall(tc) + "]"
	case "search_files":
		var args struct {
			Query   string `json:"query"`
			Pattern string `json:"pattern"`
		}
		if json.Unmarshal([]byte(tc.Args), &args) == nil {
			q := args.Query
			if q == "" {
				q = args.Pattern
			}
			if len(q) > 60 {
				q = q[:60]
			}
			return "[Search: " + q + "]"
		}
		return "[Search]"
	default:
		return "[" + tc.Name + "]"
	}
}

func buildTurnBlock(turn Turn, num int) string {
	var sb strings.Builder

	userMsg := turn.UserMessage
	if userMsg == "" {
		userMsg = "(继续)"
	}
	sb.WriteString(fmt.Sprintf("### [%d] User\n%s\n", num, userMsg))

	texts := turn.AssistantTexts
	if text := lastMeaningfulText(texts); text != "" {
		sb.WriteString("Assistant\n")
		sb.WriteString(text)
		sb.WriteString("\n")
	} else if len(texts) > 0 {
		sb.WriteString("Assistant\n")
		sb.WriteString(texts[len(texts)-1])
		sb.WriteString("\n")
	}

	var toolLines []string
	for _, tc := range turn.ToolCalls {
		if skipTools[tc.Name] {
			continue
		}
		desc := describeToolCall(tc)
		if desc == "" {
			continue
		}
		line := desc
		if tc.Result != "" && (fileEditTools[tc.Name] || strings.HasPrefix(desc, "[Edit") || strings.HasPrefix(desc, "[Write")) {
			lines := strings.Split(tc.Result, "\n")
			brief := strings.Join(lines[:min(len(lines), 5)], "\n")
			if len(brief) > 300 {
				brief = brief[:200] + "..."
			}
			line += "\n" + brief
		}
		toolLines = append(toolLines, line)
	}
	if len(toolLines) > 0 {
		sb.WriteString("Tools\n")
		sb.WriteString(strings.Join(toolLines, "\n"))
		sb.WriteString("\n")
	}

	return sb.String()
}

func buildCompressedTurn(turn Turn, num int) string {
	userMsg := strings.ReplaceAll(turn.UserMessage, "\n", " ")
	if userMsg == "" {
		userMsg = "(继续)"
	}
	if len(userMsg) > 80 {
		userMsg = userMsg[:70] + "..."
	}

	var asstBrief string
	if text := lastMeaningfulText(turn.AssistantTexts); text != "" {
		flat := strings.ReplaceAll(text, "\n", " ")
		if len(flat) > 120 {
			flat = flat[:100] + "..."
		}
		asstBrief = flat
	}
	if asstBrief == "" {
		asstBrief = "..."
	}

	return fmt.Sprintf("### [%d] User: %s\nAssistant: %s\n", num, userMsg, asstBrief)
}

func GenerateCompress(session *Session, maxTokens int) string {
	turns := session.Turns
	var validTurns []Turn
	for _, t := range turns {
		if t.UserMessage != "" || len(t.AssistantTexts) > 0 || len(t.ToolCalls) > 0 {
			validTurns = append(validTurns, t)
		}
	}

	var header strings.Builder
	header.WriteString("# Codex 会话上下文\n\n")
	header.WriteString(fmt.Sprintf("项目: %s | 模型: %s | 时间: %s\n\n", session.Meta.Cwd, session.Meta.Model, session.Meta.Timestamp))

	task := ""
	for _, t := range validTurns {
		if t.UserMessage != "" {
			task = t.UserMessage
			break
		}
	}
	header.WriteString("## 任务\n" + task + "\n\n")

	if len(validTurns) > 0 {
		last := validTurns[len(validTurns)-1]
		if text := lastMeaningfulText(last.AssistantTexts); text != "" {
			header.WriteString("## 当前状态\n" + text + "\n\n")
		}
	}

	if pending := extractPendingItems(validTurns); len(pending) > 0 {
		header.WriteString("## 待办\n")
		for _, p := range pending {
			header.WriteString("- [ ] " + p + "\n")
		}
		header.WriteString("\n")
	}

	if changes := extractFileChanges(validTurns, session.Meta.Cwd); len(changes) > 0 {
		header.WriteString("## 文件变更\n")
		for _, c := range changes {
			header.WriteString(c + "\n")
		}
		header.WriteString("\n")
	}

	if decisions := extractDecisions(validTurns); len(decisions) > 0 {
		header.WriteString("## 关键决策\n")
		for _, d := range decisions {
			header.WriteString("- " + d + "\n")
		}
		header.WriteString("\n")
	}

	if work := extractWorkDone(validTurns, session.Meta.Cwd); len(work) > 0 {
		header.WriteString("## 已完成\n")
		for i, w := range work {
			header.WriteString(fmt.Sprintf("%d. %s\n", i+1, w))
		}
		header.WriteString("\n")
	}

	headerTokens := EstimateTokens(header.String())
	separatorTokens := EstimateTokens("\n## 对话历史\n")
	remainingBudget := maxTokens - headerTokens - separatorTokens
	if remainingBudget < 1000 {
		remainingBudget = 1000
	}

	type turnBlock struct {
		num        int
		block      string
		tokens     int
		priority   int
		compressed string
	}

	var blocks []turnBlock
	for i, turn := range validTurns {
		num := i + 1
		block := buildTurnBlock(turn, num)
		tokens := EstimateTokens(block)

		hasEdits := false
		for _, tc := range turn.ToolCalls {
			if fileEditTools[tc.Name] {
				hasEdits = true
				break
			}
			if tc.Name == "exec_command" {
				var args struct {
					Cmd string `json:"cmd"`
				}
				if json.Unmarshal([]byte(tc.Args), &args) == nil && len(detectFileEditsFromCommand(args.Cmd)) > 0 {
					hasEdits = true
					break
				}
			}
		}
		priority := i
		if hasEdits {
			priority += len(validTurns)
		}

		compressed := buildCompressedTurn(turn, num)
		blocks = append(blocks, turnBlock{num: num, block: block, tokens: tokens, priority: priority, compressed: compressed})
	}

	sort.Slice(blocks, func(i, j int) bool { return blocks[i].priority > blocks[j].priority })

	selected := make(map[int]bool)
	useCompressed := make(map[int]bool)
	for _, b := range blocks {
		if remainingBudget <= 0 {
			break
		}
		if b.tokens <= remainingBudget {
			selected[b.num] = true
			remainingBudget -= b.tokens
		} else {
			compTokens := EstimateTokens(b.compressed)
			ratio := float64(remainingBudget) / float64(b.tokens)
			if ratio > 0.3 && compTokens <= remainingBudget {
				selected[b.num] = true
				useCompressed[b.num] = true
				remainingBudget -= compTokens
			}
		}
	}

	var sb strings.Builder
	sb.WriteString(header.String())
	sb.WriteString("## 对话历史\n\n")

	sort.Slice(blocks, func(i, j int) bool { return blocks[i].num < blocks[j].num })

	for _, b := range blocks {
		if !selected[b.num] {
			continue
		}
		if useCompressed[b.num] {
			sb.WriteString(b.compressed)
			sb.WriteString("\n")
		} else {
			sb.WriteString(b.block)
			sb.WriteString("\n")
		}
	}

	omitted := len(validTurns) - len(selected)
	if omitted > 0 {
		sb.WriteString(fmt.Sprintf("\n> 已省略 %d 轮低优先级对话（共 %d 轮）\n", omitted, len(validTurns)))
	}

	usedTokens := EstimateTokens(sb.String())
	sb.WriteString(fmt.Sprintf("\n> 估算 token 数: ~%d / %d\n", usedTokens, maxTokens))

	return sb.String()
}

// --- search ---

type SearchResult struct {
	SessionID string
	Title     string
	Cwd       string
	UpdatedAt string
	Matches   []string
}

func SearchSessions(keyword, cwdFilter string, sinceDays int) ([]SearchResult, error) {
	sessions, err := QueryAllSessions(sinceDays)
	if err != nil {
		return nil, err
	}

	kw := strings.ToLower(keyword)
	var results []SearchResult

	for _, s := range sessions {
		if cwdFilter != "" && !strings.HasPrefix(s.Cwd, cwdFilter) {
			continue
		}

		matches := searchFile(s.RolloutPath, kw)
		if len(matches) > 0 {
			results = append(results, SearchResult{
				SessionID: s.ID,
				Title:     s.Title,
				Cwd:       s.Cwd,
				UpdatedAt: s.UpdatedAt,
				Matches:   matches,
			})
		}
	}

	return results, nil
}

func searchFile(filePath, keyword string) []string {
	f, err := os.Open(filePath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var matches []string
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var obj struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal([]byte(line), &obj) != nil {
			continue
		}

		if obj.Type != "response_item" && obj.Type != "event_msg" {
			continue
		}

		lower := strings.ToLower(line)
		if !strings.Contains(lower, keyword) {
			continue
		}

		var p struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content any    `json:"content"`
			Message string `json:"message"`
			Name    string `json:"name"`
			Args    string `json:"arguments"`
		}
		if json.Unmarshal(obj.Payload, &p) != nil {
			continue
		}

		var text string
		switch {
		case p.Role == "user" && p.Content != nil:
			text = contentToString(p.Content)
		case p.Role == "assistant" && p.Content != nil:
			text = contentToString(p.Content)
		case p.Type == "function_call":
			text = p.Name + " " + p.Args
		case p.Message != "":
			text = p.Message
		}

		text = strings.ToLower(text)
		if strings.Contains(text, keyword) {
			snippet := extractSnippet(text, keyword, 80)
			if !seen[snippet] {
				seen[snippet] = true
				matches = append(matches, snippet)
				if len(matches) >= 5 {
					return matches
				}
			}
		}
	}

	return matches
}

func contentToString(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return fmt.Sprintf("%v", content)
}

func extractSnippet(text, keyword string, maxLen int) string {
	idx := strings.Index(text, keyword)
	if idx == -1 {
		if len(text) > maxLen {
			return text[:maxLen] + "..."
		}
		return text
	}

	start := idx - maxLen/3
	if start < 0 {
		start = 0
	}
	end := start + maxLen
	if end > len(text) {
		end = len(text)
	}

	snippet := text[start:end]
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(text) {
		snippet = snippet + "..."
	}
	return snippet
}

// --- stats ---

type StatsGroup struct {
	Key    string
	Count  int
	Tokens int64
}

type StatsResult struct {
	TotalSessions int
	TotalTokens   int64
	ProjectCount  int
	ByProject     []StatsGroup
	ByModel       []StatsGroup
	ByDay         []StatsGroup
}

func AnalyzeStats(sinceDays int) (*StatsResult, error) {
	sessions, err := QueryAllSessions(sinceDays)
	if err != nil {
		return nil, err
	}

	result := &StatsResult{
		TotalSessions: len(sessions),
	}

	projects := make(map[string]bool)
	projectTokens := make(map[string]int64)
	projectCount := make(map[string]int)
	dayTokens := make(map[string]int64)
	dayCount := make(map[string]int)

	for _, s := range sessions {
		result.TotalTokens += s.TokensUsed

		cwd := s.Cwd
		if cwd == "" {
			cwd = "(unknown)"
		}
		projects[cwd] = true
		projectTokens[cwd] += s.TokensUsed
		projectCount[cwd]++

		day := s.UpdatedAt
		if len(day) >= 10 {
			day = day[:10]
		}
		dayTokens[day] += s.TokensUsed
		dayCount[day]++
	}

	result.ProjectCount = len(projects)

	for cwd, count := range projectCount {
		result.ByProject = append(result.ByProject, StatsGroup{
			Key:    cwd,
			Count:  count,
			Tokens: projectTokens[cwd],
		})
	}
	sort.Slice(result.ByProject, func(i, j int) bool { return result.ByProject[i].Tokens > result.ByProject[j].Tokens })

	modelInfo, err := collectModelInfo(sessions)
	if err == nil {
		for model, ms := range modelInfo {
			result.ByModel = append(result.ByModel, StatsGroup{
				Key:    model,
				Count:  ms.count,
				Tokens: ms.tokens,
			})
		}
		sort.Slice(result.ByModel, func(i, j int) bool { return result.ByModel[i].Tokens > result.ByModel[j].Tokens })
	}

	for day, count := range dayCount {
		result.ByDay = append(result.ByDay, StatsGroup{
			Key:    day,
			Count:  count,
			Tokens: dayTokens[day],
		})
	}
	sort.Slice(result.ByDay, func(i, j int) bool { return result.ByDay[i].Key > result.ByDay[j].Key })

	return result, nil
}

type modelStats struct {
	count  int
	tokens int64
}

func collectModelInfo(sessions []SessionEntry) (map[string]*modelStats, error) {
	result := make(map[string]*modelStats)

	limit := len(sessions)
	if limit > 50 {
		limit = 50
	}

	for _, s := range sessions[:limit] {
		if s.RolloutPath == "" {
			continue
		}
		session, err := ParseSession(s.RolloutPath)
		if err != nil {
			continue
		}
		model := session.Meta.Model
		if model == "" {
			model = "(unknown)"
		}
		if idx := strings.LastIndex(model, "/"); idx >= 0 {
			model = model[idx+1:]
		}
		if stat, ok := result[model]; ok {
			stat.count++
			stat.tokens += s.TokensUsed
		} else {
			result[model] = &modelStats{count: 1, tokens: s.TokensUsed}
		}
	}

	if limit < len(sessions) && len(result) > 0 {
		scale := float64(len(sessions)) / float64(limit)
		for _, stat := range result {
			stat.count = int(float64(stat.count) * scale)
			stat.tokens = int64(float64(stat.tokens) * scale)
		}
	}

	return result, nil
}

// --- diff ---

type FileChange struct {
	Path    string
	Action  string
	Summary string
}

func ExtractDiff(session *Session, verbose bool) []FileChange {
	seen := make(map[string]*FileChange)
	cwd := session.Meta.Cwd

	for _, turn := range session.Turns {
		for _, tc := range turn.ToolCalls {
			switch tc.Name {
			case "apply_patch":
				p := extractFilePathFromToolCall(tc)
				if !isValidProjectPath(p, cwd) {
					continue
				}
				summary := ""
				if verbose && tc.Result != "" {
					summary = summarizePatch(tc.Result)
				}
				updateFileChange(seen, p, "修改", summary)

			case "write_file", "create_file":
				p := extractFilePathFromToolCall(tc)
				if !isValidProjectPath(p, cwd) {
					continue
				}
				summary := ""
				if verbose {
					summary = summarizeWrite(tc.Args)
				}
				updateFileChange(seen, p, "新建", summary)

			case "exec_command":
				var args struct {
					Cmd string `json:"cmd"`
				}
				if json.Unmarshal([]byte(tc.Args), &args) != nil {
					continue
				}
				edits := detectFileEditsFromCommand(args.Cmd)
				for _, e := range edits {
					if !isValidProjectPath(e.Path, cwd) {
						continue
					}
					summary := ""
					if verbose && tc.Result != "" {
						summary = summarizeCommand(tc.Result)
					}
					updateFileChange(seen, e.Path, e.Action, summary)
				}
			}
		}
	}

	var changes []FileChange
	for _, c := range seen {
		changes = append(changes, *c)
	}
	return changes
}

func updateFileChange(seen map[string]*FileChange, path, action, summary string) {
	if existing, ok := seen[path]; ok {
		if action == "新建" {
			existing.Action = "新建"
		}
		if summary != "" && existing.Summary == "" {
			existing.Summary = summary
		}
	} else {
		seen[path] = &FileChange{Path: path, Action: action, Summary: summary}
	}
}

func summarizePatch(result string) string {
	lines := strings.Split(result, "\n")
	added, removed := 0, 0
	for _, l := range lines {
		if strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++") {
			added++
		} else if strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---") {
			removed++
		}
	}
	return fmt.Sprintf("+%d/-%d lines", added, removed)
}

func summarizeWrite(args string) string {
	var parsed struct {
		Content string `json:"content"`
	}
	if json.Unmarshal([]byte(args), &parsed) == nil && parsed.Content != "" {
		lines := strings.Count(parsed.Content, "\n") + 1
		return fmt.Sprintf("%d lines", lines)
	}
	return ""
}

func summarizeCommand(result string) string {
	lines := strings.Split(result, "\n")
	if len(lines) > 3 {
		return fmt.Sprintf("%d lines output", len(lines))
	}
	brief := strings.Join(lines[:min(len(lines), 3)], "; ")
	if len(brief) > 100 {
		brief = brief[:100] + "..."
	}
	return brief
}

// --- clean ---

type CleanOptions struct {
	DryRun        bool
	OlderThanDays int
	ArchivedOnly  bool
}

type CleanFileResult struct {
	Path string
	Size int64
}

type CleanResult struct {
	Files     []CleanFileResult
	TotalSize int64
}

func CleanOldSessions(opts CleanOptions) (*CleanResult, error) {
	cutoff := time.Now().AddDate(0, 0, -opts.OlderThanDays)
	home := codexHome()

	var dirs []string
	if opts.ArchivedOnly {
		dirs = append(dirs, filepath.Join(home, "archived_sessions"))
	} else {
		dirs = append(dirs,
			filepath.Join(home, "archived_sessions"),
			filepath.Join(home, "sessions"),
		)
	}

	result := &CleanResult{}

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}

			info, err := e.Info()
			if err != nil {
				continue
			}

			if info.ModTime().After(cutoff) {
				continue
			}

			path := filepath.Join(dir, e.Name())
			result.Files = append(result.Files, CleanFileResult{Path: path, Size: info.Size()})
			result.TotalSize += info.Size()

			if !opts.DryRun {
				os.Remove(path)
			}
		}
	}

	if !opts.DryRun && len(result.Files) > 0 {
		cleanSQLiteRecords(result.Files)
	}

	return result, nil
}

func cleanSQLiteRecords(files []CleanFileResult) {
	dbPath := StateDBPath()
	if _, err := os.Stat(dbPath); err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, f := range files {
		cmd := exec.CommandContext(ctx, "sqlite3", dbPath,
			fmt.Sprintf("DELETE FROM threads WHERE rollout_path = '%s'", f.Path))
		cmd.Run()
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}