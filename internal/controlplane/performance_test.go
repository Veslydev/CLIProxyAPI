package controlplane

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func BenchmarkControlPlaneNativeSnapshotRouting(b *testing.B) {
	s, err := Open(filepath.Join(b.TempDir(), "state.sqlite"))
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	candidates := make([]*auth.Auth, 128)
	for i := range candidates {
		candidates[i] = &auth.Auth{ID: fmt.Sprintf("account-%03d", i), Provider: "codex"}
	}
	if err = s.SyncAccounts(candidates); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err = s.FilterCandidates(context.Background(), "model", executor.Options{}, candidates); err != nil {
			b.Fatal(err)
		}
	}
}

func TestControlPlaneLargeHistoryMeasurement(t *testing.T) {
	if os.Getenv("CP_PERF_SMOKE") == "" {
		t.Skip("set CP_PERF_SMOKE=1 for bounded history measurement")
	}
	s, _ := testStore(t)
	ctx := context.Background()
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	statement, err := tx.Prepare("INSERT INTO requests(id,at,provider,model,account,pool,api_key,status,tokens,latency,data) VALUES(?,?,?,?,?,?,?,?,?,?,?)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = statement.Close() }()
	for i := 0; i < 100000; i++ {
		at := s.now().Add(-time.Duration(i) * time.Minute)
		r := RequestLog{ID: fmt.Sprintf("large-%06d", i), At: at, Provider: "codex", Model: fmt.Sprintf("model-%d", i%10), Account: fmt.Sprintf("account-%d", i%100), Pool: "pool", APIKey: "key", Status: 200, Total: 100, LatencyMS: 250}
		data, err := encode(r)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = statement.Exec(r.ID, at.Unix(), r.Provider, r.Model, r.Account, r.Pool, r.APIKey, r.Status, r.Total, r.LatencyMS, data); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 10000; i++ {
		if _, err = tx.Exec("INSERT INTO rollups VALUES(?,?,?,?,?,?,?,?,?,?,?)", s.now().AddDate(0, 0, -i/100).Format("2006-01-02"), "codex", fmt.Sprintf("model-%d", i%10), fmt.Sprintf("account-%d", i%100), "pool", "key", 10, 1, 1000, 2500, .01); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	stats := s.db.Stats()
	measure := func(name string, fn func() error) {
		var timings []time.Duration
		for range 25 {
			start := time.Now()
			if err := fn(); err != nil {
				t.Fatal(err)
			}
			timings = append(timings, time.Since(start))
		}
		sort.Slice(timings, func(i, j int) bool { return timings[i] < timings[j] })
		t.Logf("%s: n=25 p50=%s p95=%s max=%s", name, timings[12], timings[23], timings[24])
	}
	measure("request history unfiltered 100-row page", func() error {
		rows, err := s.RequestLogs(ctx, LogFilter{Limit: 100})
		if err == nil && len(rows) != 100 {
			return fmt.Errorf("wrong history page length")
		}
		return err
	})
	measure("request history account/model filter", func() error {
		rows, err := s.RequestLogs(ctx, LogFilter{Account: "account-7", Model: "model-7", Limit: 100})
		if err == nil && len(rows) != 100 {
			return fmt.Errorf("wrong filtered page length")
		}
		return err
	})
	measure("analytics full totals/daily/100 groups", func() error {
		page, err := s.AnalyticsPage(ctx, s.now().AddDate(0, 0, -100).Format("2006-01-02"), "", LogFilter{Limit: 100})
		if err == nil && (page.Groups != 10000 || page.Totals.Requests != 100000) {
			return fmt.Errorf("wrong analytics totals")
		}
		return err
	})
	end := s.db.Stats()
	var bytes int64
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if stat, err := os.Stat(s.path + suffix); err == nil {
			bytes += stat.Size()
		}
	}
	t.Logf("history dataset=100000 raw rows/10000 rollup groups, DB+WAL+SHM=%d bytes, host=%s/%s CPUs=%d; query wait count=%d duration=%s, unexpected errors=0; isolated temporary database cleaned by test", bytes, runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), end.WaitCount-stats.WaitCount, end.WaitDuration-stats.WaitDuration)
}

func TestCompactionPreservesAccountingAndBoundedDashboardQueries(t *testing.T) {
	s, _ := testStore(t)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := range 600 {
		id := fmt.Sprintf("request-%d", i)
		entry := RequestLog{ID: id, At: s.now(), Provider: "codex", Account: "fixture", Model: "model", Status: 200}
		data, err := encode(entry)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("INSERT INTO requests(id,at,provider,model,account,data) VALUES(?,?,?,?,?,?)", id, s.now().Unix(), "codex", "model", "fixture", data); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("INSERT INTO settlements(id) VALUES(?)", id); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ limit, want int }{{0, 100}, {500, 500}, {10000, 100}} {
		rows, err := s.RequestLogs(context.Background(), LogFilter{Account: "fixture", Limit: tc.limit})
		if err != nil || len(rows) != tc.want {
			t.Fatalf("bounded query %d: %d %v", tc.limit, len(rows), err)
		}
	}
	seen := map[string]bool{}
	filter := LogFilter{Account: "fixture", Limit: 73}
	for {
		rows, err := s.RequestLogs(context.Background(), filter)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			if seen[row.ID] {
				t.Fatalf("pagination duplicated %s", row.ID)
			}
			seen[row.ID] = true
		}
		last := rows[len(rows)-1]
		filter.Before, filter.BeforeID = last.At.Unix(), last.ID
	}
	if len(seen) != 600 {
		t.Fatalf("same-second pagination lost rows: %d", len(seen))
	}
	if _, err = s.Compact(); err != nil {
		t.Fatal(err)
	}
	now := s.now().Add(40 * 24 * time.Hour)
	s.now = func() time.Time { return now }
	if err = s.Retain(30); err != nil {
		t.Fatal(err)
	}
	var settlements int
	if err = s.db.QueryRow("SELECT count(*) FROM settlements").Scan(&settlements); err != nil || settlements != 600 {
		t.Fatalf("compaction/retention lost deduplication: %d %v", settlements, err)
	}
}

func TestAnalyticsFiltersApplyBeforeRowLimit(t *testing.T) {
	s, _ := testStore(t)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for i := range 5001 {
		if _, err = tx.Exec("INSERT INTO rollups VALUES(?,?,?,?,?,?,?,?,?,?,?)", "2026-10-03", "codex", fmt.Sprintf("model-%d", i), "account", "pool", "key", 1, 0, 1, 10, 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec("INSERT INTO rollups VALUES(?,?,?,?,?,?,?,?,?,?,?)", "2026-10-02", "claude", "wanted", "account", "pool", "key", 2, 1, 3, 20, .5); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []LogFilter{{Provider: "claude"}, {Model: "wanted"}, {Provider: "claude", Account: "account", Pool: "pool", APIKey: "key"}} {
		for _, until := range []string{"", "2026-10-02"} {
			rows, err := s.AnalyticsFiltered(context.Background(), "2026-10-01", until, f)
			if err != nil || len(rows) != 1 || rows[0].Model != "wanted" {
				t.Fatalf("filtered analytics: %#v %v", rows, err)
			}
		}
	}
	rows, err := s.AnalyticsFiltered(context.Background(), "2026-10-01", "", LogFilter{Provider: "claude' OR 1=1 --"})
	if err != nil || len(rows) != 0 {
		t.Fatalf("filter is not bound: %v %v", rows, err)
	}
	page, err := s.AnalyticsPage(context.Background(), "2026-10-01", "", LogFilter{Limit: 100})
	if err != nil || len(page.Rows) != 100 || page.Groups != 5002 || page.Totals.Requests != 5003 || page.NextOffset != 100 {
		t.Fatalf("analytics full totals: %#v %v", page, err)
	}
	last, err := s.AnalyticsPage(context.Background(), "2026-10-01", "", LogFilter{Limit: 100, Offset: 5000})
	if err != nil || len(last.Rows) != 2 || last.NextOffset != -1 {
		t.Fatalf("analytics last page: %#v %v", last, err)
	}
}

func TestHistoryPaginationAllKinds(t *testing.T) {
	s, _ := testStore(t)
	for _, kind := range []string{"decisions", "quota", "warm", "audit"} {
		for i := range 205 {
			var err error
			switch kind {
			case "decisions":
				_, err = s.db.Exec("INSERT INTO decisions(at,data) VALUES(?,?)", s.now().Unix(), fmt.Sprintf(`{"n":%d}`, i))
			case "quota":
				_, err = s.db.Exec("INSERT INTO quota_history(account,at,data) VALUES(?,?,?)", "test", s.now().Unix(), fmt.Sprintf(`{"n":%d}`, i))
			case "warm":
				_, err = s.db.Exec("INSERT INTO warm_runs(at,account,reason,result) VALUES(?,?,?,?)", s.now().Unix(), "test", "test", fmt.Sprint(i))
			case "audit":
				_, err = s.db.Exec("INSERT INTO audit(at,action,target) VALUES(?,?,?)", s.now().Unix(), "test", fmt.Sprint(i))
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		count, before := 0, int64(0)
		seen := map[string]bool{}
		for {
			page, err := s.HistoryPage(context.Background(), kind, "test", before, 73)
			if err != nil {
				t.Fatal(err)
			}
			count += len(page.Rows)
			for _, row := range page.Rows {
				if seen[string(row)] {
					t.Fatalf("%s duplicate history row: %s", kind, row)
				}
				seen[string(row)] = true
			}
			if page.Next == 0 {
				break
			}
			if before != 0 && page.Next >= before {
				t.Fatal("history cursor did not advance")
			}
			before = page.Next
		}
		if count != 205 {
			t.Fatalf("%s history truncated: %d", kind, count)
		}
	}
}
