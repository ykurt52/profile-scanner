package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type User struct {
	Login   string    `json:"login"`
	Name    string    `json:"name"`
	Bio     string    `json:"bio"`
	Company string    `json:"company"`
	Place   string    `json:"location"`
	Blog    string    `json:"blog"`
	Avatar  string    `json:"avatar_url"`
	URL     string    `json:"html_url"`
	Repos   int       `json:"public_repos"`
	Fans    int       `json:"followers"`
	Created time.Time `json:"created_at"`
	Type    string    `json:"type"`
}

type Repo struct {
	Name     string    `json:"name"`
	Full     string    `json:"full_name"`
	URL      string    `json:"html_url"`
	Desc     string    `json:"description"`
	Fork     bool      `json:"fork"`
	Archived bool      `json:"archived"`
	Lang     string    `json:"language"`
	Topics   []string  `json:"topics"`
	Stars    int       `json:"stargazers_count"`
	Forks    int       `json:"forks_count"`
	Size     int       `json:"size"` // KB
	Branch   string    `json:"default_branch"`
	Pushed   time.Time `json:"pushed_at"`
	Created  time.Time `json:"created_at"`
}

type Node struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int    `json:"size"`
}

type Tree struct {
	Nodes     []Node `json:"tree"`
	Truncated bool   `json:"truncated"`
}

func esc(s string) string { return url.PathEscape(s) }

func (c *Client) User(ctx context.Context, login string) (*User, error) {
	var u User
	if err := c.GetJSON(ctx, "/users/"+esc(login), &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// Repos lists the user's own public repos, at most pages pages of 100.
func (c *Client) Repos(ctx context.Context, login string, pages int) ([]Repo, error) {
	var all []Repo
	for p := 1; p <= pages; p++ {
		var batch []Repo
		path := fmt.Sprintf("/users/%s/repos?type=owner&sort=pushed&per_page=100&page=%d", esc(login), p)
		if err := c.GetJSON(ctx, path, &batch); err != nil {
			return all, err
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return all, nil
}

func (c *Client) Langs(ctx context.Context, full string) (map[string]int64, error) {
	m := map[string]int64{}
	err := c.GetJSON(ctx, "/repos/"+full+"/languages", &m)
	return m, err
}

func (c *Client) Readme(ctx context.Context, full string) (string, error) {
	b, err := c.GetRaw(ctx, "/repos/"+full+"/readme")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (c *Client) Tree(ctx context.Context, full, branch string) (*Tree, error) {
	path := "/repos/" + full + "/git/trees/" + esc(branch) + "?recursive=1"
	b, err := c.Get(ctx, path, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	var t Tree
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (c *Client) File(ctx context.Context, full, path, ref string) (string, error) {
	parts := strings.Split(path, "/")
	for i := range parts {
		parts[i] = esc(parts[i])
	}
	p := "/repos/" + full + "/contents/" + strings.Join(parts, "/") + "?ref=" + url.QueryEscape(ref)
	b, err := c.GetRaw(ctx, p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
