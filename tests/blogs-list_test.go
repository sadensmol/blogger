package tests_test

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"go.yaml.in/yaml/v3"

	controllercli "github.com/sadensmol/blogger/internal/controller/cli"
	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/feed"
	"github.com/sadensmol/blogger/internal/repository"
	"github.com/sadensmol/blogger/internal/service"
	"github.com/sadensmol/blogger/tests"
	testsrepository "github.com/sadensmol/blogger/tests/repository"
)

const (
	blogsListPath = "../blogs.yaml"
	// AWS What's New shares its host with the old AWS News Blog entry, but it is its own blog (PLAN.md D2).
	awsWhatsNewPage = domain.URL("https://aws.amazon.com/new")
)

type blogsListFile struct {
	Sources []blogsListEntry `yaml:"sources"`
}

type blogsListEntry struct {
	ID     int64  `yaml:"id"`
	URL    string `yaml:"url"`
	Feed   string `yaml:"feed"`
	Title  string `yaml:"title"`
	Status string `yaml:"status"`
	Origin string `yaml:"origin"`
	Up     int    `yaml:"up"`
	Down   int    `yaml:"down"`
}

var oldBlogs = []blogsListEntry{
	{URL: "https://matklad.github.io/"},
	{URL: "https://fabiensanglard.net/"},
	{URL: "https://www.langchain.com/blog", Feed: "https://www.langchain.com/blog/rss.xml"},
	{URL: "https://stripe.dev/blog"},
	{URL: "https://keenagi.com/"},
	{URL: "https://boristane.com/blog"},
	{URL: "https://www.conductor.build/blog"},
	{URL: "https://coder.com/blog"},
	{URL: "https://go.dev/blog/"},
	{URL: "https://victoriametrics.com/blog/index.html"},
	{URL: "https://overreacted.io/"},
	{URL: "https://aws.amazon.com/blogs/aws/", Feed: "https://aws.amazon.com/blogs/aws/feed/"},
	{URL: "https://monzo.com/blog/topic/technology"},
	{URL: "https://paulhammant.com/"},
	{URL: "https://martinfowler.com/bliki/", Feed: "https://martinfowler.com/bliki/bliki.atom"},
	{URL: "https://www.ybrikman.com/blog/"},
	{URL: "https://www.kitchensoap.com/"},
	{URL: "https://p.umputun.com/"},
	{URL: "https://addyosmani.com/blog/"},
	{URL: "https://empryo.com/blog"},
	{URL: "https://superplane.com/blog/"},
	{URL: "https://claude.com/resources/articles", Title: "Claude articles"},
	{URL: "https://www.greptile.com/blog", Title: "Greptile blog"},
	{URL: "https://www.gruntwork.io/blog", Title: "Gruntwork blog"},
	{URL: "https://jvns.ca/", Feed: "https://jvns.ca/atom.xml"},
	{URL: "https://research.swtch.com/", Feed: "https://research.swtch.com/feed.atom"},
	{URL: "https://lethain.com/", Feed: "https://lethain.com/feeds"},
	{URL: "https://fasterthanli.me/", Feed: "https://fasterthanli.me/index.xml"},
	{URL: "https://blog.cloudflare.com/", Feed: "https://blog.cloudflare.com/rss"},
}

var newBlogs = []blogsListEntry{
	{Title: "Applied Go", URL: "https://appliedgo.net", Feed: "https://appliedgo.net/index.xml"},
	{Title: "OpenAI", URL: "https://openai.com/blog", Feed: "https://openai.com/blog/rss.xml"},
	{Title: "Google DeepMind", URL: "https://deepmind.google/blog", Feed: "https://deepmind.google/blog/rss.xml"},
	{Title: "Simon Willison", URL: "https://simonwillison.net", Feed: "https://simonwillison.net/atom/everything/"},
	{Title: "Armin Ronacher", URL: "https://lucumr.pocoo.org", Feed: "https://lucumr.pocoo.org/feed.atom"},
	{Title: "Sequoia Capital", URL: "https://www.sequoiacap.com/stories/", Feed: "https://www.sequoiacap.com/feed/"},
	{Title: "Y Combinator", URL: "https://www.ycombinator.com/blog", Feed: "https://www.ycombinator.com/blog/rss"},
	{Title: "The Rundown AI", URL: "https://www.therundown.ai", Feed: "https://www.therundown.ai/feed"},
	{Title: "Technical Writing", URL: "https://technicalwriting.dev", Feed: "https://technicalwriting.dev/rss.xml"},
	{Title: "Anton Zhiyanov", URL: "https://antonz.org", Feed: "https://antonz.org/index.xml"},
	{Title: "Three Dots Labs", URL: "https://threedots.tech", Feed: "https://threedots.tech/index.xml"},
	{Title: "Alex Edwards Go", URL: "https://www.alexedwards.net/blog", Feed: "https://www.alexedwards.net/static/feed.rss"},
	{Title: "Bitfield Consulting", URL: "https://bitfieldconsulting.com/posts", Feed: "https://bitfieldconsulting.com/posts?format=rss"},
	{Title: "Eli Bendersky", URL: "https://eli.thegreenplace.net", Feed: "https://eli.thegreenplace.net/feeds/all.atom.xml"},
	{Title: "Ben Hoyt", URL: "https://benhoyt.com/writings/", Feed: "https://benhoyt.com/writings/rss.xml"},
	{Title: "Dave Cheney", URL: "https://dave.cheney.net", Feed: "https://dave.cheney.net/feed"},
	{Title: "Ardan Labs", URL: "https://www.ardanlabs.com/blog", Feed: "https://www.ardanlabs.com/blog/index.xml"},
	{Title: "Go Beyond", URL: "https://boldlygo.tech", Feed: "https://boldlygo.tech/index.xml"},
	{Title: "JetBrains Go", URL: "https://blog.jetbrains.com/go/", Feed: "https://blog.jetbrains.com/go/feed/"},
	{Title: "Marc Brooker", URL: "https://brooker.co.za/blog", Feed: "https://brooker.co.za/blog/rss.xml"},
	{Title: "Murat Demirbas", URL: "https://muratbuffalo.blogspot.com", Feed: "https://muratbuffalo.blogspot.com/feeds/posts/default"},
	{Title: "Aphyr", URL: "https://aphyr.com/posts", Feed: "https://aphyr.com/posts.atom"},
	{Title: "Brendan Gregg", URL: "https://www.brendangregg.com/blog", Feed: "https://www.brendangregg.com/blog/rss.xml"},
	{Title: "Jane Street", URL: "https://blog.janestreet.com", Feed: "https://blog.janestreet.com/feed.xml"},
	{Title: "Oxide Computer", URL: "https://oxide.computer/blog", Feed: "https://oxide.computer/blog/feed"},
	{Title: "ClickHouse", URL: "https://clickhouse.com/blog", Feed: "https://clickhouse.com/rss.xml"},
	{Title: "Neon", URL: "https://neon.com/blog", Feed: "https://neon.com/blog/rss.xml"},
	{Title: "PlanetScale", URL: "https://planetscale.com/blog", Feed: "https://planetscale.com/blog/rss.xml"},
	{Title: "Supabase", URL: "https://supabase.com/blog", Feed: "https://supabase.com/rss.xml"},
	{Title: "Planet PostgreSQL", URL: "https://planet.postgresql.org", Feed: "https://planet.postgresql.org/rss20.xml"},
	{Title: "Redis", URL: "https://redis.io/blog/", Feed: "https://redis.io/blog/feed/"},
	{Title: "Kubernetes Blog", URL: "https://kubernetes.io/blog/", Feed: "https://kubernetes.io/feed.xml"},
	{Title: "InfoQ Architecture", URL: "https://www.infoq.com/architecture-design/", Feed: "https://feed.infoq.com/architecture-design/"},
	{Title: "Nelson Elhage", URL: "https://blog.nelhage.com", Feed: "https://blog.nelhage.com/atom.xml"},
	{Title: "Netflix Tech", URL: "https://netflixtechblog.com", Feed: "https://netflixtechblog.com/feed"},
	{Title: "Stripe", URL: "https://stripe.com/blog", Feed: "https://stripe.com/blog/feed.rss"},
	{Title: "GitHub Engineering", URL: "https://github.blog/engineering", Feed: "https://github.blog/engineering/feed/"},
	{Title: "Meta Engineering", URL: "https://engineering.fb.com", Feed: "https://engineering.fb.com/feed/"},
	{Title: "Dropbox Tech", URL: "https://dropbox.tech", Feed: "https://dropbox.tech/feed"},
	{Title: "Slack Engineering", URL: "https://slack.engineering", Feed: "https://slack.engineering/feed/"},
	{Title: "Spotify Engineering", URL: "https://engineering.atspotify.com", Feed: "https://engineering.atspotify.com/feed"},
	{Title: "Airbnb Engineering", URL: "https://medium.com/airbnb-engineering", Feed: "https://medium.com/feed/airbnb-engineering"},
	{Title: "Discord Engineering", URL: "https://discord.com/blog", Feed: "https://discord.com/blog/rss.xml"},
	{Title: "Figma Engineering", URL: "https://www.figma.com/blog/", Feed: "https://www.figma.com/blog/feed/atom.xml"},
	{Title: "Etsy Engineering", URL: "https://www.etsy.com/codeascraft", Feed: "https://www.etsy.com/codeascraft/rss"},
	{Title: "Gitlab Engineering", URL: "https://about.gitlab.com/blog/", Feed: "https://about.gitlab.com/atom.xml"},
	{Title: "Google Research", URL: "https://research.google/blog/", Feed: "https://research.google/blog/rss/"},
	{Title: "Google Testing Blog", URL: "https://testing.googleblog.com", Feed: "https://testing.googleblog.com/feeds/posts/default"},
	{Title: "Tailscale", URL: "https://tailscale.com/blog", Feed: "https://tailscale.com/blog/index.xml"},
	{Title: "Fly.io", URL: "https://fly.io/blog", Feed: "https://fly.io/blog/feed.xml"},
	{Title: "Vercel", URL: "https://vercel.com/blog", Feed: "https://vercel.com/atom"},
	{Title: "Sentry", URL: "https://blog.sentry.io", Feed: "https://blog.sentry.io/feed.xml"},
	{Title: "Grafana", URL: "https://grafana.com/blog/", Feed: "https://grafana.com/blog/index.xml"},
	{Title: "Honeycomb", URL: "https://www.honeycomb.io/blog", Feed: "https://www.honeycomb.io/feed"},
	{Title: "Datadog", URL: "https://www.datadoghq.com/blog/", Feed: "https://www.datadoghq.com/blog/index.xml"},
	{Title: "HashiCorp", URL: "https://www.hashicorp.com/blog", Feed: "https://www.hashicorp.com/blog/feed.xml"},
	{Title: "Docker", URL: "https://www.docker.com/blog/", Feed: "https://www.docker.com/blog/feed/"},
	{Title: "Zed", URL: "https://zed.dev/blog", Feed: "https://zed.dev/blog.rss"},
	{Title: "Val Town", URL: "https://blog.val.town", Feed: "https://blog.val.town/rss.xml"},
	{Title: "Deno", URL: "https://deno.com/blog", Feed: "https://deno.com/feed"},
	{Title: "Bun", URL: "https://bun.sh/blog", Feed: "https://bun.sh/rss.xml"},
	{Title: "Rust Blog", URL: "https://blog.rust-lang.org", Feed: "https://blog.rust-lang.org/feed.xml"},
	{Title: "Hugging Face", URL: "https://huggingface.co/blog", Feed: "https://huggingface.co/blog/feed.xml"},
	{Title: "Lilian Weng", URL: "https://lilianweng.github.io", Feed: "https://lilianweng.github.io/index.xml"},
	{Title: "Sebastian Raschka", URL: "https://magazine.sebastianraschka.com", Feed: "https://magazine.sebastianraschka.com/feed"},
	{Title: "Latent Space", URL: "https://www.latent.space", Feed: "https://www.latent.space/feed"},
	{Title: "Interconnects", URL: "https://www.interconnects.ai", Feed: "https://www.interconnects.ai/feed"},
	{Title: "Eugene Yan", URL: "https://eugeneyan.com/writing/", Feed: "https://eugeneyan.com/rss/"},
	{Title: "ElevenLabs", URL: "https://elevenlabs.io/blog", Feed: "https://elevenlabs.io/blog/rss.xml"},
	{Title: "Pragmatic Engineer", URL: "https://blog.pragmaticengineer.com", Feed: "https://blog.pragmaticengineer.com/rss/"},
	{Title: "Dan Luu", URL: "https://danluu.com", Feed: "https://danluu.com/atom.xml"},
	{Title: "Thorsten Ball", URL: "https://registerspill.thorstenball.com", Feed: "https://registerspill.thorstenball.com/feed"},
	{Title: "Mitchell Hashimoto", URL: "https://mitchellh.com/writing", Feed: "https://mitchellh.com/feed.xml"},
	{Title: "tonsky", URL: "https://tonsky.me", Feed: "https://tonsky.me/atom.xml"},
	{Title: "Xe Iaso", URL: "https://xeiaso.net/blog", Feed: "https://xeiaso.net/blog.rss"},
	{Title: "Hillel Wayne", URL: "https://www.hillelwayne.com/post/", Feed: "https://www.hillelwayne.com/index.xml"},
	{Title: "Charity Majors", URL: "https://charity.wtf", Feed: "https://charity.wtf/feed/"},
	{Title: "Drew DeVault", URL: "https://drewdevault.com/blog", Feed: "https://drewdevault.com/blog/index.xml"},
	{Title: "antirez", URL: "https://antirez.com", Feed: "https://antirez.com/rss"},
	{Title: "Chris Siebenmann", URL: "https://utcc.utoronto.ca/~cks/space/blog/", Feed: "https://utcc.utoronto.ca/~cks/space/blog/?atom"},
	{Title: "ByteByteGo", URL: "https://blog.bytebytego.com", Feed: "https://blog.bytebytego.com/feed"},
	{Title: "Bruce Schneier", URL: "https://www.schneier.com", Feed: "https://www.schneier.com/feed/atom/"},
	{Title: "Troy Hunt", URL: "https://www.troyhunt.com", Feed: "https://feeds.feedburner.com/TroyHunt"},
	{Title: "Signal Blog", URL: "https://signal.org/blog/", Feed: "https://signal.org/blog/rss.xml"},
	{Title: "AWS What's New", URL: "https://aws.amazon.com/new", Feed: "https://aws.amazon.com/about-aws/whats-new/recent/feed/"},
	{Title: "Moritz Plassnig", URL: "https://moritzplassnig.com", Feed: "https://moritzplassnig.com/rss/"},
}

type BlogsListTestSuite struct {
	suite.Suite

	entries []blogsListEntry
}

func TestBlogsListTestSuite(t *testing.T) {
	suite.Run(t, &BlogsListTestSuite{})
}

func (s *BlogsListTestSuite) SetupTest() {
	f, err := os.Open(blogsListPath)
	s.Require().NoError(err)
	defer func() {
		s.Require().NoError(f.Close())
	}()

	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(true)
	var file blogsListFile
	s.Require().NoError(decoder.Decode(&file))
	s.entries = file.Sources
}

func (s *BlogsListTestSuite) parse(raw string) domain.URL {
	if raw == "" {
		return ""
	}
	u, err := domain.ParseURL(raw)
	s.Require().NoError(err)
	return u
}

func (s *BlogsListTestSuite) TestBlogsListHoldsEveryNewBlog() {
	byPage := make(map[domain.URL]blogsListEntry, len(s.entries))
	for _, entry := range s.entries {
		byPage[s.parse(entry.URL)] = entry
	}

	var missing []string
	for _, want := range newBlogs {
		got, ok := byPage[s.parse(want.URL)]
		if !ok {
			missing = append(missing, want.URL)
			continue
		}
		s.Equal(s.parse(want.Feed), s.parse(got.Feed), want.URL)
		s.Equal(want.Title, got.Title, want.URL)
	}

	s.Empty(missing)
}

func (s *BlogsListTestSuite) requireEveryNewBlog() {
	pages := make(map[domain.URL]bool, len(s.entries))
	for _, entry := range s.entries {
		pages[s.parse(entry.URL)] = true
	}

	var missing []string
	for _, want := range newBlogs {
		if !pages[s.parse(want.URL)] {
			missing = append(missing, want.URL)
		}
	}
	s.Require().Empty(missing, "the list lacks these new blogs")
}

func (s *BlogsListTestSuite) TestBlogsListKeepsOldEntriesAndAppendsNewBlogs() {
	s.requireEveryNewBlog()

	byPage := make(map[domain.URL]blogsListEntry, len(s.entries))
	for _, entry := range s.entries {
		byPage[s.parse(entry.URL)] = entry
	}
	for _, old := range oldBlogs {
		got, ok := byPage[s.parse(old.URL)]
		if s.Truef(ok, "the old entry %s is gone", old.URL) {
			s.Equal(old, got)
		}
	}
}

func (s *BlogsListTestSuite) TestBlogsListAddsNoSecondEntryForAKnownBlog() {
	s.requireEveryNewBlog()

	byPage := make(map[domain.URL]blogsListEntry, len(s.entries))
	for _, entry := range s.entries {
		byPage[s.parse(entry.URL)] = entry
	}
	for _, want := range newBlogs {
		entry := byPage[s.parse(want.URL)]
		page, feed := s.parse(entry.URL), s.parse(entry.Feed)
		for _, old := range oldBlogs {
			oldPage, oldFeed := s.parse(old.URL), s.parse(old.Feed)
			if page != awsWhatsNewPage {
				s.Falsef(page.SameSiteAs(oldPage), "%s shares a host with the old entry %s", page, oldPage)
			}
			if oldFeed != "" {
				s.NotEqualf(oldFeed, feed, "%s has the feed of the old entry %s", page, oldPage)
			}
		}
	}
}

func (s *BlogsListTestSuite) TestBlogsListIsAValidImportFile() {
	s.requireEveryNewBlog()

	for i, entry := range s.entries {
		s.NotEmptyf(entry.URL, "entry %d has no url", i+1)
		_, err := domain.ParseURL(entry.URL)
		s.NoErrorf(err, "entry %d url", i+1)
		if entry.Feed != "" {
			_, err := domain.ParseURL(entry.Feed)
			s.NoErrorf(err, "entry %d feed", i+1)
		}
	}
}

func (s *BlogsListTestSuite) TestBlogsListHasNoPageOrFeedTwice() {
	s.requireEveryNewBlog()

	pages := make(map[domain.URL]int, len(s.entries))
	feeds := make(map[domain.URL]int, len(s.entries))
	for _, entry := range s.entries {
		pages[s.parse(entry.URL)]++
		if entry.Feed != "" {
			feeds[s.parse(entry.Feed)]++
		}
	}

	var twice []domain.URL
	for u, n := range pages {
		if n > 1 {
			twice = append(twice, u)
		}
	}
	for u, n := range feeds {
		if n > 1 {
			twice = append(twice, u)
		}
	}
	s.Empty(twice)
}

type BlogsListImportTestSuite struct {
	tests.Suite

	sources *testsrepository.Source
	svc     *service.Source
}

func TestBlogsListImportTestSuite(t *testing.T) {
	suite.Run(t, &BlogsListImportTestSuite{})
}

func (s *BlogsListImportTestSuite) SetupTest() {
	s.Suite.SetupTest()
	s.sources = testsrepository.NewSource(s.DB)
	// every feed lookup fails at once: the list gives each new blog its feed, so the import must not look one up.
	s.svc = service.NewSource(repository.NewSource(s.DB), feed.NewClient(time.Nanosecond), s.Clock.Now)
}

func (s *BlogsListImportTestSuite) TestImportDryRunOfBlogsListAddsOnlyTheNewBlogs() {
	f, err := os.Open(blogsListPath)
	s.Require().NoError(err)
	defer func() {
		s.Require().NoError(f.Close())
	}()
	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(true)
	var file blogsListFile
	s.Require().NoError(decoder.Decode(&file))

	listed := make(map[domain.URL]bool, len(oldBlogs)+len(newBlogs))
	for _, blog := range slices.Concat(oldBlogs, newBlogs) {
		page, err := domain.ParseURL(blog.URL)
		s.Require().NoError(err)
		listed[page] = true
	}
	board := slices.Clone(oldBlogs)
	for _, entry := range file.Sources {
		page, err := domain.ParseURL(entry.URL)
		s.Require().NoError(err)
		if !listed[page] {
			board = append(board, entry)
		}
	}
	for _, blog := range board {
		page, err := domain.ParseURL(blog.URL)
		s.Require().NoError(err)
		var blogFeed domain.URL
		if blog.Feed != "" {
			blogFeed, err = domain.ParseURL(blog.Feed)
			s.Require().NoError(err)
		}
		_, err = s.sources.Insert(s.Ctx, testsrepository.SourceRow{
			SiteURL: string(page), FeedURL: string(blogFeed), Title: blog.Title, Status: cmp.Or(blog.Status, string(domain.SourceActive)),
		}, s.Clock.Now())
		s.Require().NoError(err)
	}

	var out bytes.Buffer
	err = controllercli.Sources(s.Ctx, controllercli.SourcesData{
		Svc: s.svc, Args: []string{"import", blogsListPath}, Out: &out,
	})

	s.Require().NoError(err)
	s.NotContains(out.String(), "~ change")
	s.NotContains(out.String(), "- remove")
	for _, want := range newBlogs {
		page, err := domain.ParseURL(want.URL)
		s.Require().NoError(err)
		newFeed, err := domain.ParseURL(want.Feed)
		s.Require().NoError(err)
		s.Contains(out.String(), fmt.Sprintf("+ add     %s (feed %s)\n", page, newFeed))
	}
	s.Equal(len(board), s.CountRows("source"))
}

func (s *BlogsListImportTestSuite) TestImportRejectsAMisspeltKey() {
	const misspelt = "sources:\n  - url: https://appliedgo.net\n    feeds: https://appliedgo.net/index.xml\n    title: Applied Go\n"

	var out bytes.Buffer
	err := controllercli.Sources(s.Ctx, controllercli.SourcesData{
		Svc: s.svc, Args: []string{"import", "--apply", "-"}, In: strings.NewReader(misspelt), Out: &out,
	})

	var typeErr *yaml.TypeError
	s.ErrorAs(err, &typeErr)
	s.Empty(out.String())
	s.Equal(0, s.CountRows("source"))
}
