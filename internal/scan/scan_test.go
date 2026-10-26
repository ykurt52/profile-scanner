package scan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"skillscanner/internal/analyze"
	"skillscanner/internal/github"
	"skillscanner/internal/report"
)

// fake mimics the GitHub API. Tests never talk to the real GitHub.
type fake struct {
	t     *testing.T
	limit int
	left  atomic.Int64
	hits  atomic.Int64 // requests that counted against the quota (not 304)
	total atomic.Int64
	busy  atomic.Int64
	peak  atomic.Int64
	deny  atomic.Bool
	mu    sync.Mutex
	paths []string
}

func newFake(t *testing.T, limit, left int) (*fake, *httptest.Server) {
	f := &fake{t: t, limit: limit}
	f.left.Store(int64(left))
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fake) serve(w http.ResponseWriter, r *http.Request) {
	n := f.busy.Add(1)
	defer f.busy.Add(-1)
	for {
		m := f.peak.Load()
		if n <= m || f.peak.CompareAndSwap(m, n) {
			break
		}
	}
	f.total.Add(1)
	f.mu.Lock()
	f.paths = append(f.paths, r.URL.RequestURI())
	f.mu.Unlock()

	if r.URL.Path == "/rate_limit" { // free
		json.NewEncoder(w).Encode(map[string]any{"resources": map[string]any{"core": map[string]any{
			"limit": f.limit, "remaining": f.left.Load(), "reset": time.Now().Add(time.Hour).Unix()}}})
		return
	}
	if f.deny.Load() {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"You have exceeded a secondary rate limit"}`))
		return
	}

	body, status := f.route(r)
	etag := fmt.Sprintf(`"%x"`, len(body)*31+len(r.URL.Path))
	rate := func() {
		w.Header().Set("X-RateLimit-Limit", fmt.Sprint(f.limit))
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprint(f.left.Load()))
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(time.Now().Add(time.Hour).Unix()))
		w.Header().Set("X-RateLimit-Resource", "core")
	}
	if status == 200 && r.Header.Get("If-None-Match") == etag {
		rate()
		w.WriteHeader(http.StatusNotModified) // GitHub: a 304 with a token is free
		return
	}
	f.hits.Add(1)
	f.left.Add(-1)
	rate()
	if status != 200 {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("ETag", etag)
	w.Write([]byte(body))
}

var (
	recent = time.Now().Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	old    = time.Now().Add(-5 * 365 * 24 * time.Hour).Format(time.RFC3339)
)

func (f *fake) route(r *http.Request) (string, int) {
	p := r.URL.Path
	raw := strings.Contains(r.Header.Get("Accept"), "raw")
	switch {
	case p == "/users/ayse":
		return `{"login":"ayse","name":"Ayse Test","bio":"Embedded developer","public_repos":5,"html_url":"https://github.com/ayse"}`, 200
	case p == "/users/ayse/repos":
		return `[
 {"name":"ayse","full_name":"ayse/ayse","description":"My profile","fork":false,"size":5,"default_branch":"main","pushed_at":"` + recent + `"},
 {"name":"esp32-greenhouse","full_name":"ayse/esp32-greenhouse","description":"ESP32 based greenhouse irrigation controller with MQTT","fork":false,"language":"C++","topics":["iot","esp32","agriculture"],"stargazers_count":12,"size":300,"default_branch":"main","pushed_at":"` + recent + `"},
 {"name":"shop-front","full_name":"ayse/shop-front","description":"E-commerce storefront","fork":false,"language":"TypeScript","topics":["react","ecommerce"],"stargazers_count":3,"size":900,"default_branch":"main","pushed_at":"` + recent + `"},
 {"name":"old-ml","full_name":"ayse/old-ml","description":"Patient readmission prediction","fork":false,"language":"Jupyter Notebook","topics":[],"stargazers_count":0,"size":200,"default_branch":"master","pushed_at":"` + old + `"},
 {"name":"forked-lib","full_name":"ayse/forked-lib","fork":true,"language":"Go","size":100,"default_branch":"main","pushed_at":"` + recent + `"}
]`, 200
	case p == "/repos/ayse/esp32-greenhouse/languages":
		return `{"C++":80000,"C":15000,"CMake":2000}`, 200
	case p == "/repos/ayse/shop-front/languages":
		return `{"TypeScript":120000,"CSS":30000,"HTML":5000}`, 200
	case p == "/repos/ayse/old-ml/languages":
		return `{"Jupyter Notebook":200000,"Python":20000}`, 200
	case p == "/repos/ayse/esp32-greenhouse/readme" && raw:
		return "# Greenhouse\nSoil sensor + irrigation for farming. Built with PlatformIO and FreeRTOS.", 200
	case p == "/repos/ayse/shop-front/readme" && raw:
		return "# Shop\nA web app with cart and checkout.", 200
	case p == "/repos/ayse/old-ml/readme":
		return "", 404
	case p == "/repos/ayse/esp32-greenhouse/git/trees/main":
		return `{"tree":[{"path":"platformio.ini","type":"blob","size":300},{"path":"src/main.cpp","type":"blob","size":5000},{"path":"include/sensors.h","type":"blob","size":800}]}`, 200
	case p == "/repos/ayse/shop-front/git/trees/main":
		return `{"tree":[{"path":"package.json","type":"blob","size":900},{"path":"vite.config.ts","type":"blob","size":200},{"path":"node_modules/x/package.json","type":"blob","size":10}]}`, 200
	case p == "/repos/ayse/old-ml/git/trees/master":
		return `{"tree":[{"path":"requirements.txt","type":"blob","size":100},{"path":"analysis.ipynb","type":"blob","size":90000}]}`, 200
	case p == "/repos/ayse/esp32-greenhouse/contents/platformio.ini" && raw:
		return "[env:esp32dev]\nplatform = espressif32\nframework = arduino\nlib_deps = knolleary/PubSubClient", 200
	case p == "/repos/ayse/shop-front/contents/package.json" && raw:
		return `{"dependencies":{"react":"^18","react-dom":"^18"},"devDependencies":{"vite":"^5","tailwindcss":"^3"}}`, 200
	case p == "/repos/ayse/old-ml/contents/requirements.txt" && raw:
		return "pandas==2.0\nscikit-learn>=1.3\nmatplotlib", 200
	}
	f.t.Errorf("unexpected request: %s", r.URL.RequestURI())
	return "", 404
}

var dir string

func client(srv *httptest.Server, token string, budget int) *github.Client {
	c := github.New(token, github.NewCache(dir))
	c.Base = srv.URL
	c.Delay = 0
	c.Budget = budget
	return c
}

func noop(string, int, int) {}

func TestEndToEnd(t *testing.T) {
	dir = t.TempDir()
	f, srv := newFake(t, 5000, 5000)
	c := client(srv, "test-token", 300)
	ctx := context.Background()

	plan, err := Precheck(ctx, c, "https://github.com/ayse/", 30, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.OK || plan.Repos != 5 || plan.Cost != 1+5*PerRepo {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	p, err := Run(ctx, c, plan, noop)
	if err != nil {
		t.Fatal(err)
	}
	if used := int(f.hits.Load()); used > plan.Cost+1 { // +1: user request in Precheck
		t.Fatalf("spent more than planned: used %d, planned %d", used, plan.Cost)
	}
	if f.peak.Load() > 1 {
		t.Fatalf("parallel requests sent: %d", f.peak.Load())
	}
	if p.Stats.Analyzed != 3 || p.Stats.Forks != 1 {
		t.Fatalf("wrong repo counts: %+v", p.Stats)
	}

	repos := map[string]analyze.Result{}
	for _, r := range p.Repos {
		repos[r.Name] = r
	}
	if _, ok := repos["ayse"]; ok {
		t.Error("profile README repo must not be scanned")
	}
	top := func(repo, want string, got []analyze.Score) {
		t.Helper()
		if len(got) == 0 || got[0].Name != want {
			t.Errorf("%s: expected %q first, got %+v", repo, want, got)
		}
	}
	top("esp32-greenhouse", "Embedded / IoT", repos["esp32-greenhouse"].Purposes)
	top("shop-front", "Web Frontend", repos["shop-front"].Purposes)
	top("old-ml", "Data Science / AI", repos["old-ml"].Purposes)
	top("esp32-greenhouse", "Agriculture", repos["esp32-greenhouse"].Sectors)
	top("shop-front", "E-commerce / Retail", repos["shop-front"].Sectors)
	top("old-ml", "Healthcare", repos["old-ml"].Sectors)
	if l := p.Langs[0].Name; l != "C++" && l != "TypeScript" {
		t.Errorf("top language should come from a recent repo, got %s", l)
	}

	html, err := report.Render(p)
	if err != nil {
		t.Fatal(err)
	}
	if out := os.Getenv("SAMPLE_REPORT"); out != "" {
		_ = os.WriteFile(out, html, 0o644) // sample report for a visual check
	}
	for _, want := range []string{"Ayse Test", "Embedded / IoT", "esp32-greenhouse", "Agriculture"} {
		if !strings.Contains(string(html), want) {
			t.Errorf("report is missing %q", want)
		}
	}

	// Second run: data unchanged -> 304 -> almost nothing is spent.
	before := f.hits.Load()
	c2 := client(srv, "test-token", 300)
	plan2, err := Precheck(ctx, c2, "ayse", 30, 20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, c2, plan2, noop); err != nil {
		t.Fatal(err)
	}
	if extra := f.hits.Load() - before; extra > 1 { // only the missing README 404 counts again
		t.Fatalf("cache did not work: second run spent %d requests", extra)
	}
	if c2.Hits() < 10 {
		t.Fatalf("too few cache hits: %d", c2.Hits())
	}
}

func TestBudgetStopsScan(t *testing.T) {
	dir = t.TempDir()
	f, srv := newFake(t, 5000, 5000)
	c := client(srv, "test-token", 8) // tiny budget
	plan, err := Precheck(context.Background(), c, "ayse", 30, 20)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Repos != 1 { // (8 - 1 user) - 1 page = 6 -> 1 repo
		t.Fatalf("budget should allow 1 repo: %+v", plan)
	}
	if _, err := Run(context.Background(), c, plan, noop); err != nil {
		t.Fatal(err)
	}
	if got := f.hits.Load(); got > 8 {
		t.Fatalf("budget exceeded: %d requests", got)
	}
}

func TestReserveRespected(t *testing.T) {
	dir = t.TempDir()
	// No token: limit 60, 15 left, reserve 20% = 12 -> only 3 requests may be spent.
	f, srv := newFake(t, 60, 15)
	c := client(srv, "", 300)
	plan, err := Precheck(context.Background(), c, "ayse", 30, 20)
	if err != nil {
		t.Fatal(err)
	}
	if plan.OK {
		t.Fatalf("scan must not be allowed with too little quota: %+v", plan)
	}
	// Even when the client is called directly, it must not go below the reserve.
	for i := 0; i < 10; i++ {
		_, err := c.Langs(context.Background(), "ayse/shop-front")
		if errors.Is(err, github.ErrReserve) {
			break
		}
	}
	if f.left.Load() < 12 {
		t.Fatalf("reserve broken: %d left", f.left.Load())
	}
}

func TestStopsOn403WithoutRetry(t *testing.T) {
	dir = t.TempDir()
	f, srv := newFake(t, 5000, 5000)
	c := client(srv, "test-token", 300)
	plan, err := Precheck(context.Background(), c, "ayse", 30, 20)
	if err != nil {
		t.Fatal(err)
	}
	f.deny.Store(true)
	before := f.total.Load()
	_, err = Run(context.Background(), c, plan, noop)
	if !errors.Is(err, github.ErrLimited) {
		t.Fatalf("expected a rate-limit error, got: %v", err)
	}
	if n := f.total.Load() - before; n != 1 {
		t.Fatalf("retried after 403: %d requests", n)
	}
}

func TestBadUsernameSendsNothing(t *testing.T) {
	dir = t.TempDir()
	f, srv := newFake(t, 5000, 5000)
	c := client(srv, "", 300)
	if _, err := Precheck(context.Background(), c, "bad user/name", 30, 20); err == nil {
		t.Fatal("expected an error")
	}
	if f.total.Load() != 0 {
		t.Fatal("no request should be sent for an invalid username")
	}
}
