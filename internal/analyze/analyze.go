// Package analyze builds a language / purpose / sector profile from collected repo data.
// It runs fully offline and sends no network requests.
package analyze

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"skillscanner/internal/github"
)

// Data is the raw data collected from GitHub for one repo.
type Data struct {
	Repo    github.Repo
	Langs   map[string]int64
	Readme  string
	Paths   []string          // lowercase file paths
	Files   map[string]string // manifest path -> content
	Partial bool              // some data was skipped to protect the quota
}

type Score struct {
	Name     string   `json:"name"`
	Score    float64  `json:"score"`
	Pct      float64  `json:"percent"`
	Evidence []string `json:"evidence"`
}

type Lang struct {
	Name   string  `json:"name"`
	Bytes  int64   `json:"bytes"`
	Pct    float64 `json:"byte_percent"`
	Weight float64 `json:"weighted_percent"`
	Repos  int     `json:"repos"`
	Markup bool    `json:"markup"`
	Color  string  `json:"color"`
}

type Result struct {
	Name     string    `json:"name"`
	URL      string    `json:"url"`
	Desc     string    `json:"description"`
	Topics   []string  `json:"topics"`
	Stars    int       `json:"stars"`
	Pushed   time.Time `json:"pushed_at"`
	Archived bool      `json:"archived"`
	Main     string    `json:"primary_language"`
	Langs    []Lang    `json:"languages"`
	Purposes []Score   `json:"purposes"`
	Sectors  []Score   `json:"sectors"`
	Weight   float64   `json:"weight"`
	Partial  bool      `json:"partial"`
	Deps     []string  `json:"key_dependencies"`
	Head     string    `json:"-"` // start of the README
}

type Profile struct {
	User     github.User `json:"user"`
	Date     time.Time   `json:"generated_at"`
	Repos    []Result    `json:"repos"`
	Langs    []Lang      `json:"languages"`
	Purposes []Score     `json:"purposes"`
	Sectors  []Score     `json:"sectors"`
	Stats    Stats       `json:"stats"`
	Notes    []string    `json:"notes"`
	AI       *AI         `json:"ai,omitempty"`
}

type Stats struct {
	Public   int   `json:"total_public_repos"`
	Listed   int   `json:"listed_repos"`
	Forks    int   `json:"forks_skipped"`
	Analyzed int   `json:"analyzed_repos"`
	Bytes    int64 `json:"total_code_bytes"`
	Stars    int   `json:"total_stars"`
	Used     int   `json:"requests_used"`
	Hits     int   `json:"cache_hits"`
	Left     int   `json:"rate_remaining"`
	Limit    int   `json:"rate_limit"`
}

// ---- matchers ----

type matcher struct {
	cat Category
	res []*regexp.Regexp
}

func compile(cats []Category) []matcher {
	out := make([]matcher, len(cats))
	for i, c := range cats {
		m := matcher{cat: c}
		for _, w := range c.Words {
			re := regexp.MustCompile(`(?:^|[^\p{L}\p{N}])` + regexp.QuoteMeta(strings.ToLower(w)) + `(?:$|[^\p{L}\p{N}])`)
			m.res = append(m.res, re)
		}
		out[i] = m
	}
	return out
}

var (
	purposeRules = compile(Purposes)
	sectorRules  = compile(Sectors)
	tokenRe      = regexp.MustCompile(`[a-z0-9@._/\-]+`)
)

func tokens(files map[string]string) map[string]bool {
	set := map[string]bool{}
	for _, s := range files {
		for _, t := range tokenRe.FindAllString(strings.ToLower(s), -1) {
			set[strings.Trim(t, "./-")] = true
		}
	}
	return set
}

func hasDep(set map[string]bool, dep string) bool {
	if set[dep] {
		return true
	}
	for t := range set {
		if strings.HasSuffix(t, "/"+dep) || strings.HasPrefix(t, dep+".") || strings.HasPrefix(t, dep+"/") {
			return true
		}
	}
	return false
}

func ignored(p string) bool {
	for _, s := range []string{"node_modules/", "vendor/", "third_party/", "thirdparty/", "external/", ".git/"} {
		if strings.HasPrefix(p, s) || strings.Contains(p, "/"+s) {
			return true
		}
	}
	return false
}

func hasPath(paths []string, pat string) bool {
	if suf, ok := strings.CutPrefix(pat, "suffix:"); ok {
		for _, p := range paths {
			if strings.HasSuffix(p, suf) && !ignored(p) {
				return true
			}
		}
		return false
	}
	for _, p := range paths {
		if strings.Contains(p, pat) && !ignored(p) {
			return true
		}
	}
	return false
}

type text struct {
	strong string // name, description, topics
	readme string
}

// score rates one repo against one category and returns the evidence.
func score(m matcher, txt text, deps map[string]bool, paths []string, share map[string]float64, code bool) (float64, []string) {
	var total float64
	var ev []string
	add := func(p float64, sum *float64, cap float64, e string) {
		if *sum >= cap {
			return
		}
		*sum += p
		total += p
		ev = append(ev, e)
	}
	if code {
		var d, f, l float64
		for _, dep := range m.cat.Deps {
			if hasDep(deps, dep) {
				add(3, &d, 9, "dep: "+dep)
			}
		}
		for _, pat := range m.cat.Files {
			if hasPath(paths, pat) {
				add(2, &f, 6, "file: "+strings.TrimPrefix(pat, "suffix:"))
			}
		}
		for _, name := range m.cat.Langs {
			if share[name] >= 0.15 {
				add(1.5, &l, 3, "lang: "+name)
			}
		}
	}
	var k float64
	for i, re := range m.res {
		w := m.cat.Words[i]
		if re.MatchString(txt.strong) {
			add(2, &k, 6, "desc/topic: \""+w+"\"")
		} else if re.MatchString(txt.readme) {
			add(1, &k, 6, "README: \""+w+"\"")
		}
	}
	return total, ev
}

func rank(rules []matcher, txt text, deps map[string]bool, paths []string, share map[string]float64, code bool, floor float64, top int) []Score {
	var out []Score
	for _, m := range rules {
		s, ev := score(m, txt, deps, paths, share, code)
		if s >= floor {
			out = append(out, Score{Name: m.cat.Name, Score: s, Evidence: ev})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > top {
		out = out[:top]
	}
	var sum float64
	for _, s := range out {
		sum += s.Score
	}
	for i := range out {
		out[i].Pct = out[i].Score / sum * 100
	}
	return out
}

// recency weights a repo by the date of its last push.
func recency(t, now time.Time) float64 {
	months := now.Sub(t).Hours() / 24 / 30
	switch {
	case months <= 12:
		return 1.0
	case months <= 36:
		return 0.6
	default:
		return 0.3
	}
}

func clip(s string, n int) string {
	s = strings.ToLower(s)
	if len(s) > n {
		s = s[:n]
	}
	return s
}

// dashes returns the text plus a copy with dashes and underscores turned into
// spaces, so the topic "machine-learning" also matches "machine learning".
func dashes(s string) string {
	alt := strings.NewReplacer("-", " ", "_", " ").Replace(s)
	if alt == s {
		return s
	}
	return s + " \n " + alt
}

// Repo scores a single repo.
func Repo(d Data, now time.Time) Result {
	r := d.Repo
	var total int64
	for _, b := range d.Langs {
		total += b
	}
	share := map[string]float64{}
	var langs []Lang
	for name, b := range d.Langs {
		if total > 0 {
			share[name] = float64(b) / float64(total)
		}
		langs = append(langs, Lang{Name: name, Bytes: b, Pct: share[name] * 100, Color: Color(name)})
	}
	sort.Slice(langs, func(i, j int) bool { return langs[i].Bytes > langs[j].Bytes })

	txt := text{
		strong: clip(dashes(r.Name+" \n "+r.Desc+" \n "+strings.Join(r.Topics, " ")), 6000),
		readme: clip(d.Readme, 20000),
	}
	deps := tokens(d.Files)

	res := Result{
		Name: r.Name, URL: r.URL, Desc: r.Desc, Topics: r.Topics, Stars: r.Stars,
		Pushed: r.Pushed, Archived: r.Archived, Main: r.Lang, Langs: langs, Partial: d.Partial,
	}
	res.Purposes = rank(purposeRules, txt, deps, d.Paths, share, true, 3, 3)
	// Sector: one strong word in desc/topics (2) or two README words is enough.
	res.Sectors = rank(sectorRules, txt, nil, nil, nil, false, 2, 2)

	// Key dependencies (for the report and the AI digest)
	seen := map[string]bool{}
	for _, c := range Purposes {
		for _, dep := range c.Deps {
			if !seen[dep] && hasDep(deps, dep) {
				seen[dep] = true
				res.Deps = append(res.Deps, dep)
			}
		}
	}

	size := 1.0
	if total > 0 {
		size = math.Min(1.5, math.Max(0.5, math.Log10(float64(total))/5))
	}
	res.Weight = recency(r.Pushed, now) * (1 + 0.3*math.Log10(1+float64(r.Stars))) * size
	res.Head = d.Readme
	if len(res.Head) > 1500 {
		res.Head = res.Head[:1500]
	}
	return res
}
