package llm

import (
	"context"
	"fmt"
	"time"

	"github.com/sadensmol/blogger/internal/domain"
)

// a proposed blog's title and about go into one slack section, which holds at most 3000 characters
const (
	maxBlogTitle = 150
	maxBlogAbout = 300
)

type PickInput struct {
	Candidates []domain.Article
	Profile    domain.Profile
	Count      domain.PickCount
}

type pickJobInput struct {
	Min         int             `json:"min"`
	Max         int             `json:"max"`
	InterestsMD string          `json:"interests_md"`
	Candidates  []candidateJSON `json:"candidates"`
}

type candidateJSON struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	Source    string `json:"source"`
	Published string `json:"published"`
	Excerpt   string `json:"excerpt"`
}

type domainArticle domain.Article

func (a domainArticle) toCandidateJSON() candidateJSON {
	candidate := candidateJSON{
		URL:     string(a.URL),
		Title:   a.Title,
		Source:  a.SourceTitle,
		Excerpt: a.Excerpt,
	}
	if !a.PublishedAt.IsZero() {
		candidate.Published = a.PublishedAt.UTC().Format(time.RFC3339)
	}
	return candidate
}

type pickAnswer struct {
	Picks []pickJSON `json:"picks"`
}

type pickJSON struct {
	URL         string   `json:"url"`
	Description string   `json:"description"`
	Topics      []string `json:"topics"`
	Reason      string   `json:"reason"`
}

func (p pickJSON) toDomain() (domain.Pick, error) {
	u, err := domain.ParseURL(p.URL)
	if err != nil {
		return domain.Pick{}, fmt.Errorf("bad pick url: %w", err)
	}
	return domain.Pick{URL: u, Description: p.Description, Topics: p.Topics, Reason: p.Reason}, nil
}

func (r *Runner) PickArticles(ctx context.Context, in PickInput) ([]domain.Pick, Backend, error) {
	candidates := make([]candidateJSON, 0, len(in.Candidates))
	for _, article := range in.Candidates {
		candidates = append(candidates, domainArticle(article).toCandidateJSON())
	}
	req := jobRequest{
		skill: SkillPickArticles,
		input: pickJobInput{Min: in.Count.Min, Max: in.Count.Max, InterestsMD: string(in.Profile), Candidates: candidates},
	}

	var picks []domain.Pick
	backend, err := r.run(ctx, req, func(raw []byte) error {
		var answer pickAnswer
		if err := decodeAnswer(raw, &answer); err != nil {
			return err
		}
		answered := make([]domain.Pick, 0, len(answer.Picks))
		for _, p := range answer.Picks {
			pick, err := p.toDomain()
			if err != nil {
				return err
			}
			answered = append(answered, pick)
		}
		if err := domain.ValidatePicks(answered, in.Candidates, in.Count); err != nil {
			return err
		}
		picks = answered
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return picks, backend, nil
}

type profileAnswer struct {
	InterestsMD string `json:"interests_md"`
}

type updateJobInput struct {
	InterestsMD string     `json:"interests_md"`
	Votes       []voteJSON `json:"votes"`
}

type voteJSON struct {
	Vote   string   `json:"vote"`
	Title  string   `json:"title"`
	URL    string   `json:"url"`
	Source string   `json:"source"`
	Topics []string `json:"topics"`
}

type domainVotedArticle domain.VotedArticle

func (v domainVotedArticle) toVoteJSON() voteJSON {
	vote := "down"
	if v.Vote == domain.VoteUp {
		vote = "up"
	}
	return voteJSON{
		Vote:   vote,
		Title:  v.Article.Title,
		URL:    string(v.Article.URL),
		Source: v.Article.SourceTitle,
		Topics: v.Topics,
	}
}

func (r *Runner) UpdateProfile(ctx context.Context, p domain.Profile, votes []domain.VotedArticle) (domain.Profile, Backend, error) {
	votesJSON := make([]voteJSON, 0, len(votes))
	for _, v := range votes {
		votesJSON = append(votesJSON, domainVotedArticle(v).toVoteJSON())
	}
	req := jobRequest{
		skill: SkillUpdateProfile,
		input: updateJobInput{InterestsMD: string(p), Votes: votesJSON},
	}

	var updated domain.Profile
	backend, err := r.run(ctx, req, func(raw []byte) error {
		var answer profileAnswer
		if err := decodeAnswer(raw, &answer); err != nil {
			return err
		}
		profile, err := domain.Profile(answer.InterestsMD).WithNotesFrom(p)
		if err != nil {
			return err
		}
		updated = profile
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return updated, backend, nil
}

type SourceSummary struct {
	Source       domain.Source
	RecentTitles []string
}

type initJobInput struct {
	InterestsMD string              `json:"interests_md"`
	Sources     []sourceSummaryJSON `json:"sources"`
}

type sourceSummaryJSON struct {
	Title        string   `json:"title"`
	SiteURL      string   `json:"site_url"`
	RecentTitles []string `json:"recent_titles"`
}

type domainSourceSummary SourceSummary

func (s domainSourceSummary) toSourceSummaryJSON() sourceSummaryJSON {
	return sourceSummaryJSON{Title: s.Source.Title, SiteURL: string(s.Source.SiteURL), RecentTitles: s.RecentTitles}
}

func (r *Runner) InitProfile(ctx context.Context, current domain.Profile, sources []SourceSummary) (domain.Profile, Backend, error) {
	summaries := make([]sourceSummaryJSON, 0, len(sources))
	for _, s := range sources {
		summaries = append(summaries, domainSourceSummary(s).toSourceSummaryJSON())
	}
	req := jobRequest{
		skill: SkillInitProfile,
		input: initJobInput{InterestsMD: string(current), Sources: summaries},
	}

	var created domain.Profile
	backend, err := r.run(ctx, req, func(raw []byte) error {
		var answer profileAnswer
		if err := decodeAnswer(raw, &answer); err != nil {
			return err
		}
		profile, err := domain.Profile(answer.InterestsMD).WithNotesFrom(current)
		if err != nil {
			return err
		}
		created = profile
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return created, backend, nil
}

type discoverJobInput struct {
	Max     int          `json:"max"`
	Sources []sourceJSON `json:"sources"`
}

type sourceJSON struct {
	Title   string `json:"title"`
	SiteURL string `json:"site_url"`
	FeedURL string `json:"feed_url"`
	Status  string `json:"status"`
	Up      int    `json:"up"`
	Down    int    `json:"down"`
}

type domainSource domain.Source

func (s domainSource) toSourceJSON() sourceJSON {
	return sourceJSON{
		Title:   s.Title,
		SiteURL: string(s.SiteURL),
		FeedURL: string(s.FeedURL),
		Status:  string(s.Status),
		Up:      s.Votes.Up,
		Down:    s.Votes.Down,
	}
}

type discoverAnswer struct {
	Blogs []blogJSON `json:"blogs"`
}

type blogJSON struct {
	SiteURL string `json:"site_url"`
	FeedURL string `json:"feed_url"`
	Title   string `json:"title"`
	About   string `json:"about"`
	Reason  string `json:"reason"`
}

func (b blogJSON) toDomain() (domain.BlogProposal, error) {
	siteURL, err := domain.ParseURL(b.SiteURL)
	if err != nil {
		return domain.BlogProposal{}, fmt.Errorf("bad site url: %w", err)
	}
	feedURL, err := domain.ParseURL(b.FeedURL)
	if err != nil {
		return domain.BlogProposal{}, fmt.Errorf("bad feed url: %w", err)
	}
	title := []rune(b.Title)
	if len(title) > maxBlogTitle {
		title = append(title[:maxBlogTitle-1], '…')
	}
	about := []rune(b.About)
	if len(about) > maxBlogAbout {
		about = append(about[:maxBlogAbout-1], '…')
	}
	return domain.BlogProposal{SiteURL: siteURL, FeedURL: feedURL, Title: string(title), About: string(about), Reason: b.Reason}, nil
}

func (r *Runner) DiscoverBlogs(ctx context.Context, sources []domain.Source, max int) ([]domain.BlogProposal, Backend, error) {
	sourcesJSON := make([]sourceJSON, 0, len(sources))
	for _, s := range sources {
		sourcesJSON = append(sourcesJSON, domainSource(s).toSourceJSON())
	}
	req := jobRequest{
		skill:    SkillDiscoverBlogs,
		needsWeb: true,
		input:    discoverJobInput{Max: max, Sources: sourcesJSON},
	}

	var proposals []domain.BlogProposal
	backend, err := r.run(ctx, req, func(raw []byte) error {
		var answer discoverAnswer
		if err := decodeAnswer(raw, &answer); err != nil {
			return err
		}
		answered := make([]domain.BlogProposal, 0, len(answer.Blogs))
		for _, b := range answer.Blogs {
			proposal, err := b.toDomain()
			if err != nil {
				return err
			}
			answered = append(answered, proposal)
		}
		proposals = answered
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return proposals, backend, nil
}
