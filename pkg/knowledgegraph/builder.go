package knowledgegraph

import "context"

type BuildOpts struct {
	RepoDir  string
	Branch   string
	OutputDir string
	Scope    *Scope
}

type UpdateOpts struct {
	RepoDir      string
	Branch       string
	OutputDir    string
	ChangedFiles []string
	Scope        *Scope
}

type Scope struct {
	Paths  []string
	Ignore []string
}

type Result struct {
	NodeCount      int
	EdgeCount      int
	CommunityCount int
}

type Builder interface {
	Build(ctx context.Context, opts BuildOpts) (*Result, error)
	Update(ctx context.Context, opts UpdateOpts) (*Result, error)
}
