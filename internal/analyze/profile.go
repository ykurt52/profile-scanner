package analyze

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"skillscanner/internal/github"
)

// Build turns analyzed repos into a profile-level summary.
func Build(user github.User, data []Data, now time.Time) *Profile {
	p := &Profile{User: user, Date: now}
	for _, d := range data {
		p.Repos = append(p.Repos, Repo(d, now))
	}
	sort.SliceStable(p.Repos, func(i, j int) bool { return p.Repos[i].Weight > p.Repos[j].Weight })

	// ---- Languages ----
	type acc struct {
		bytes  int64
		weight float64
		repos  int
	}
	langs := map[string]*acc{}
	var all, code int64
	var sum float64
	for _, r := range p.Repos {
		p.Stats.Stars += r.Stars
		var rc int64 // code bytes in this repo
		for _, l := range r.Langs {
			if !Markup[l.Name] {
				rc += l.Bytes
			}
		}
		for _, l := range r.Langs {
			a := langs[l.Name]
			if a == nil {
				a = &acc{}
				langs[l.Name] = a
			}
			a.bytes += l.Bytes
			a.repos++
			all += l.Bytes
			if Markup[l.Name] {
				continue
			}
			code += l.Bytes
			if rc > 0 {
				// Each repo votes with its own weight so one huge repo cannot dominate.
				w := float64(l.Bytes) / float64(rc) * r.Weight
				a.weight += w
				sum += w
			}
		}
	}
	for name, a := range langs {
		l := Lang{Name: name, Bytes: a.bytes, Repos: a.repos, Markup: Markup[name], Color: Color(name)}
		if all > 0 {
			l.Pct = float64(a.bytes) / float64(all) * 100
		}
		if sum > 0 {
			l.Weight = a.weight / sum * 100
		}
		p.Langs = append(p.Langs, l)
	}
	sort.Slice(p.Langs, func(i, j int) bool {
		if p.Langs[i].Weight != p.Langs[j].Weight {
			return p.Langs[i].Weight > p.Langs[j].Weight
		}
		return p.Langs[i].Bytes > p.Langs[j].Bytes
	})
	p.Stats.Bytes = code
	p.Stats.Analyzed = len(p.Repos)

	p.Purposes = merge(p.Repos, func(r Result) []Score { return r.Purposes })
	p.Sectors = merge(p.Repos, func(r Result) []Score { return r.Sectors })
	return p
}

// merge sums each repo's category split, weighted by the repo weight.
func merge(repos []Result, get func(Result) []Score) []Score {
	type acc struct {
		total float64
		repos []string
		ev    map[string]int
	}
	m := map[string]*acc{}
	var sum float64
	for _, r := range repos {
		for _, s := range get(r) {
			a := m[s.Name]
			if a == nil {
				a = &acc{ev: map[string]int{}}
				m[s.Name] = a
			}
			v := s.Pct / 100 * r.Weight
			a.total += v
			sum += v
			a.repos = append(a.repos, r.Name)
			for _, e := range s.Evidence {
				a.ev[e]++
			}
		}
	}
	var out []Score
	for name, a := range m {
		s := Score{Name: name, Score: a.total}
		if sum > 0 {
			s.Pct = a.total / sum * 100
		}
		// Most frequent evidence plus sample repos
		type kv struct {
			k string
			n int
		}
		var evs []kv
		for k, n := range a.ev {
			evs = append(evs, kv{k, n})
		}
		sort.Slice(evs, func(i, j int) bool {
			if evs[i].n != evs[j].n {
				return evs[i].n > evs[j].n
			}
			return evs[i].k < evs[j].k
		})
		for i, e := range evs {
			if i >= 5 {
				break
			}
			if e.n > 1 {
				s.Evidence = append(s.Evidence, fmt.Sprintf("%s (%d repos)", e.k, e.n))
			} else {
				s.Evidence = append(s.Evidence, e.k)
			}
		}
		s.Evidence = append(s.Evidence, "repos: "+names(a.repos, 4))
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

func names(list []string, n int) string {
	if len(list) <= n {
		return strings.Join(list, ", ")
	}
	return strings.Join(list[:n], ", ") + fmt.Sprintf(" +%d", len(list)-n)
}

var colors = map[string]string{
	"Go": "#00ADD8", "Python": "#3572A5", "JavaScript": "#f1e05a", "TypeScript": "#3178c6",
	"Java": "#b07219", "Kotlin": "#A97BFF", "C": "#555555", "C++": "#f34b7d", "C#": "#178600",
	"Rust": "#dea584", "Ruby": "#701516", "PHP": "#4F5D95", "Swift": "#F05138", "Dart": "#00B4AB",
	"HTML": "#e34c26", "CSS": "#663399", "SCSS": "#c6538c", "Shell": "#89e051", "PowerShell": "#012456",
	"Jupyter Notebook": "#DA5B0B", "R": "#198CE7", "Vue": "#41b883", "Svelte": "#ff3e00",
	"Objective-C": "#438eff", "Scala": "#c22d40", "Lua": "#000080", "Perl": "#0298c3",
	"Haskell": "#5e5086", "Elixir": "#6e4a7e", "Assembly": "#6E4C13", "VHDL": "#adb2cb",
	"Verilog": "#b2b7f8", "SystemVerilog": "#DAE1C2", "MATLAB": "#e16737", "Solidity": "#AA6746",
	"Dockerfile": "#384d54", "HCL": "#844FBA", "Makefile": "#427819", "CMake": "#DA3434",
	"GDScript": "#355570", "ShaderLab": "#222c37", "TeX": "#3D6117", "Zig": "#ec915c",
	"Batchfile": "#C1F12E", "Julia": "#a270ba", "Clojure": "#db5855", "F#": "#b845fc",
}

// Color returns the GitHub color of a language, or one derived from its name.
func Color(name string) string {
	if c, ok := colors[name]; ok {
		return c
	}
	var h uint32 = 2166136261
	for _, b := range []byte(name) {
		h = (h ^ uint32(b)) * 16777619
	}
	return fmt.Sprintf("hsl(%d, 45%%, 55%%)", h%360)
}
