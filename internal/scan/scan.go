// Package scan drives data collection from GitHub: precheck, cost estimate and scan.
package scan

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"skillscanner/internal/analyze"
	"skillscanner/internal/github"
)

const (
	PerRepo  = 5 // languages + readme + tree + up to 2 manifest files
	MaxFiles = 2
	MaxPages = 3 // list at most 300 repos
)

// Plan is the cost estimate shown to the user before a scan.
type Plan struct {
	Login   string    `json:"login"`
	Name    string    `json:"name"`
	Avatar  string    `json:"avatar_url"`
	Public  int       `json:"public_repos"`
	Pages   int       `json:"pages"`
	Repos   int       `json:"selected"`
	Wanted  int       `json:"requested"`
	Cost    int       `json:"total_requests"`
	Auth    bool      `json:"authenticated"`
	Limit   int       `json:"rate_limit"`
	Left    int       `json:"remaining"`
	Reset   time.Time `json:"reset"`
	Budget  int       `json:"budget"`
	Reserve int       `json:"reserve"`
	OK      bool      `json:"fits"`
	Message string    `json:"message"`
	user    *github.User
}

// Precheck reads the quota (free) and the user (1 request) and returns a cost estimate.
func Precheck(ctx context.Context, c *github.Client, login string, max, reserve int) (*Plan, error) {
	login = strings.TrimSpace(login)
	login = strings.TrimPrefix(login, "@")
	login = strings.TrimPrefix(login, "https://github.com/")
	login = strings.Trim(login, "/")
	if login == "" || strings.ContainsAny(login, " /?#") {
		return nil, errors.New("enter a valid GitHub username")
	}
	rate, err := c.FetchRate(ctx)
	if err != nil {
		return nil, err
	}
	c.Reserve = maxInt(5, rate.Limit*reserve/100)
	if rate.Left-1 < c.Reserve {
		return nil, fmt.Errorf("GitHub quota is below the safety reserve (%d/%d left). It resets at %s.",
			rate.Left, rate.Limit, rate.Reset.Local().Format("15:04"))
	}
	u, err := c.User(ctx, login)
	if errors.Is(err, github.ErrMissing) {
		return nil, fmt.Errorf("no GitHub user named %q", login)
	}
	if err != nil {
		return nil, err
	}
	r := c.Rate()
	p := &Plan{
		Login: u.Login, Name: u.Name, Avatar: u.Avatar, Public: u.Repos,
		Auth: c.Token != "", Limit: r.Limit, Left: r.Left, Reset: r.Reset,
		Budget: c.Budget - c.Used(), Reserve: c.Reserve, Wanted: max, user: u,
	}
	p.Pages = minInt(MaxPages, maxInt(1, (u.Repos+99)/100))
	want := minInt(u.Repos, max)
	room := minInt(p.Budget, p.Left-p.Reserve)
	p.Repos = want
	if p.Pages+want*PerRepo > room {
		p.Repos = maxInt(0, (room-p.Pages)/PerRepo)
	}
	p.Cost = p.Pages + p.Repos*PerRepo
	switch {
	case u.Repos == 0:
		p.Message = "This user has no public repos."
	case p.Repos < 1:
		p.Message = "Not enough quota or budget for this scan. Add a GitHub token or wait for the quota to reset."
	case p.Repos < want:
		p.OK = true
		p.Message = fmt.Sprintf("To protect the quota, the %d most active repos will be scanned instead of %d.", p.Repos, want)
	default:
		p.OK = true
	}
	return p, nil
}

// Progress reports scan progress.
type Progress func(msg string, done, total int)

func fatal(err error) bool {
	return errors.Is(err, github.ErrBudget) || errors.Is(err, github.ErrReserve) ||
		errors.Is(err, github.ErrLimited) || errors.Is(err, github.ErrToken) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// Run collects data within the plan limits and builds the profile.
// On quota, budget or rate-limit errors it stops and returns a partial profile.
func Run(ctx context.Context, c *github.Client, p *Plan, progress Progress) (*analyze.Profile, error) {
	if !p.OK || p.user == nil {
		return nil, errors.New(p.Message)
	}
	var notes []string

	progress("Listing repos...", 0, p.Repos)
	repos, err := c.Repos(ctx, p.Login, p.Pages)
	if err != nil && len(repos) == 0 {
		return nil, err
	}
	if err != nil {
		notes = append(notes, "Repo list is incomplete: "+err.Error())
	}

	var own []github.Repo
	forks := 0
	for _, r := range repos {
		if r.Fork {
			forks++
			continue
		}
		if r.Size == 0 {
			continue
		}
		// The user/user repo is the GitHub profile README, not a project.
		if strings.EqualFold(r.Name, p.Login) {
			notes = append(notes, fmt.Sprintf("Profile README repo (%s/%s) was not counted as a project.", p.Login, r.Name))
			continue
		}
		own = append(own, r)
	}
	sort.SliceStable(own, func(i, j int) bool { return own[i].Pushed.After(own[j].Pushed) })
	if len(own) > p.Repos {
		notes = append(notes, fmt.Sprintf("Scanned the %d most recently updated of %d repos.", p.Repos, len(own)))
		own = own[:p.Repos]
	}

	var data []analyze.Data
	stop := false
	halt := func(err error) {
		notes = append(notes, "Stopped: "+err.Error())
		stop = true
	}
	for i, r := range own {
		progress(fmt.Sprintf("Scanning %s...", r.Name), i, len(own))
		d := analyze.Data{Repo: r, Files: map[string]string{}}

		// A repo needs at least 3 requests; do not start it otherwise.
		if !c.CanSpend(3) {
			notes = append(notes, "Quota or budget limit reached; remaining repos were skipped.")
			break
		}
		langs, err := c.Langs(ctx, r.Full)
		if err != nil && fatal(err) {
			halt(err)
			break
		}
		d.Langs = langs

		readme, err := c.Readme(ctx, r.Full)
		if err != nil && fatal(err) {
			halt(err)
		}
		d.Readme = readme

		if !stop && r.Branch != "" {
			collect(ctx, c, r, &d, halt, &stop)
		}
		if stop {
			d.Partial = true
		}
		data = append(data, d)
		if stop {
			break
		}
	}
	progress("Computing results...", len(own), len(own))

	prof := analyze.Build(*p.user, data, time.Now())
	prof.Stats.Public = p.Public
	prof.Stats.Listed = len(repos)
	prof.Stats.Forks = forks
	prof.Stats.Used = c.Used()
	prof.Stats.Hits = c.Hits()
	rate := c.Rate()
	prof.Stats.Left, prof.Stats.Limit = rate.Left, rate.Limit
	if forks > 0 {
		notes = append(notes, fmt.Sprintf("Skipped %d fork(s) (someone else's code).", forks))
	}
	prof.Notes = append(prof.Notes, notes...)
	if len(data) == 0 {
		return prof, errors.New("no repo could be scanned")
	}
	return prof, nil
}

// collect fetches the file tree and a few manifest files of one repo.
func collect(ctx context.Context, c *github.Client, r github.Repo, d *analyze.Data, halt func(error), stop *bool) {
	tree, err := c.Tree(ctx, r.Full, r.Branch)
	if err != nil {
		if fatal(err) {
			halt(err)
		}
		return
	}
	for _, n := range tree.Nodes {
		if n.Type == "blob" || n.Type == "tree" {
			d.Paths = append(d.Paths, strings.ToLower(n.Path))
		}
	}
	for _, f := range manifests(tree.Nodes) {
		if !c.CanSpend(1) {
			d.Partial = true
			return
		}
		s, err := c.File(ctx, r.Full, f, r.Branch)
		if err != nil {
			if fatal(err) {
				halt(err)
				return
			}
			continue
		}
		if len(s) > 200_000 {
			s = s[:200_000]
		}
		d.Files[f] = s
	}
}

// manifests picks the most useful dependency files from a tree (root first).
func manifests(nodes []github.Node) []string {
	type cand struct {
		path  string
		prio  int
		depth int
	}
	var cs []cand
	for _, n := range nodes {
		if n.Type != "blob" || n.Size > 500_000 {
			continue
		}
		lp := strings.ToLower(n.Path)
		depth := strings.Count(lp, "/")
		if depth > 2 || strings.Contains(lp, "node_modules/") || strings.Contains(lp, "vendor/") ||
			strings.Contains(lp, "example") || strings.Contains(lp, "test") {
			continue
		}
		base := path.Base(lp)
		prio := -1
		for i, m := range analyze.Manifests {
			if base == m {
				prio = i
				break
			}
		}
		if prio < 0 && strings.HasSuffix(base, ".csproj") {
			prio = len(analyze.Manifests)
		}
		if prio >= 0 {
			cs = append(cs, cand{n.Path, prio, depth})
		}
	}
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].depth != cs[j].depth {
			return cs[i].depth < cs[j].depth
		}
		return cs[i].prio < cs[j].prio
	})
	var out []string
	seen := map[string]bool{}
	for _, c := range cs {
		b := path.Base(strings.ToLower(c.path))
		if seen[b] {
			continue // skip a second file of the same kind, prefer variety
		}
		seen[b] = true
		out = append(out, c.path)
		if len(out) >= MaxFiles {
			break
		}
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
