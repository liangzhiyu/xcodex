package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func codexHome() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(os.Getenv("HOME"), ".codex")
}

func StateDBPath() string {
	return filepath.Join(codexHome(), "state_5.sqlite")
}

func ListRecentSessions(limit int) ([]SessionEntry, error) {
	dbPath := StateDBPath()
	if _, err := os.Stat(dbPath); err == nil {
		return listFromSQLite(dbPath, limit)
	}
	return listFromFilesystem(limit)
}

func ListSessionsByCwd(cwd string, limit int) ([]SessionEntry, error) {
	dbPath := StateDBPath()
	if _, err := os.Stat(dbPath); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "sqlite3", dbPath, "-json",
			fmt.Sprintf("SELECT id, cwd, title, rollout_path, first_user_message, updated_at, tokens_used FROM threads WHERE cwd LIKE '%s%%' ORDER BY updated_at DESC LIMIT %d", cwd, limit))
		out, err := cmd.Output()
		if err != nil {
			return listFromFilesystem(limit)
		}
		return parseSQLiteRows(out)
	}
	return listFromFilesystem(limit)
}

func listFromSQLite(dbPath string, limit int) ([]SessionEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sqlite3", dbPath, "-json",
		fmt.Sprintf("SELECT id, cwd, title, rollout_path, first_user_message, updated_at, tokens_used FROM threads ORDER BY updated_at DESC LIMIT %d", limit))
	out, err := cmd.Output()
	if err != nil {
		return listFromFilesystem(limit)
	}
	return parseSQLiteRows(out)
}

func parseSQLiteRows(out []byte) ([]SessionEntry, error) {
	var rows []struct {
		ID           string  `json:"id"`
		Cwd          string  `json:"cwd"`
		Title        string  `json:"title"`
		RolloutPath  string  `json:"rollout_path"`
		FirstUserMsg string  `json:"first_user_message"`
		UpdatedAt    float64 `json:"updated_at"`
		TokensUsed   int64   `json:"tokens_used"`
	}
	if json.Unmarshal(out, &rows) != nil {
		return nil, fmt.Errorf("parse sqlite rows")
	}

	var entries []SessionEntry
	for _, r := range rows {
		title := r.Title
		if title == "" {
			title = r.FirstUserMsg
		}
		if len(title) > 80 {
			title = title[:80]
		}
		entries = append(entries, SessionEntry{
			ID:          r.ID,
			Cwd:         r.Cwd,
			Title:       title,
			RolloutPath: r.RolloutPath,
			UpdatedAt:   time.Unix(int64(r.UpdatedAt), 0).Format("2006-01-02T15:04:05"),
			TokensUsed:  r.TokensUsed,
		})
	}
	return entries, nil
}

func listFromFilesystem(limit int) ([]SessionEntry, error) {
	home := codexHome()
	dirs := []string{
		filepath.Join(home, "sessions"),
		filepath.Join(home, "archived_sessions"),
	}

	type fileInfo struct {
		path  string
		mtime time.Time
	}
	var files []fileInfo

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
			files = append(files, fileInfo{path: filepath.Join(dir, e.Name()), mtime: info.ModTime()})
		}
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].mtime.After(files[j].mtime)
	})

	var entries []SessionEntry
	for i, f := range files {
		if i >= limit {
			break
		}
		entries = append(entries, SessionEntry{
			RolloutPath: f.path,
			UpdatedAt:   f.mtime.Format("2006-01-02T15:04:05"),
		})
	}
	return entries, nil
}

func FindLatestSession() (string, error) {
	return FindSessionByIndex(1, "", 1)
}

func FindSessionByIndex(index int, cwd string, limit int) (string, error) {
	if index <= 0 {
		return "", fmt.Errorf("session index must be >= 1")
	}
	if limit <= 0 {
		limit = 15
	}

	var (
		sessions []SessionEntry
		err      error
	)
	if cwd != "" {
		sessions, err = ListSessionsByCwd(cwd, limit)
	} else {
		sessions, err = ListRecentSessions(limit)
	}
	if err != nil {
		return "", err
	}
	if index > len(sessions) {
		return "", fmt.Errorf("session index out of range: %d (only %d sessions listed)", index, len(sessions))
	}
	if sessions[index-1].RolloutPath == "" {
		return "", fmt.Errorf("session %d has empty rollout path", index)
	}
	return sessions[index-1].RolloutPath, nil
}

func FindSessionByID(id string) (string, error) {
	dbPath := StateDBPath()
	if _, err := os.Stat(dbPath); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "sqlite3", dbPath, "-json",
			fmt.Sprintf("SELECT rollout_path FROM threads WHERE id = '%s'", id))
		out, err := cmd.Output()
		if err == nil {
			var rows []struct {
				RolloutPath string `json:"rollout_path"`
			}
			if json.Unmarshal(out, &rows) == nil && len(rows) > 0 {
				if _, err := os.Stat(rows[0].RolloutPath); err == nil {
					return rows[0].RolloutPath, nil
				}
			}
		}
	}
	return "", fmt.Errorf("session not found: %s", id)
}

// QueryAllSessions returns all sessions from SQLite for stats/search.
func QueryAllSessions(sinceDays int) ([]SessionEntry, error) {
	dbPath := StateDBPath()
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("sqlite not found: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := "SELECT id, cwd, title, rollout_path, first_user_message, updated_at, tokens_used FROM threads ORDER BY updated_at DESC"
	if sinceDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -sinceDays).Unix()
		query = fmt.Sprintf("SELECT id, cwd, title, rollout_path, first_user_message, updated_at, tokens_used FROM threads WHERE updated_at >= %d ORDER BY updated_at DESC", cutoff)
	}

	cmd := exec.CommandContext(ctx, "sqlite3", dbPath, "-json", query)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("sqlite query: %w", err)
	}
	return parseSQLiteRows(out)
}