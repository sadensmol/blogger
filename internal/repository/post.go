package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sadensmol/blogger/internal/domain"
)

type Post struct {
	db *sql.DB
}

func NewPost(db *sql.DB) *Post {
	return &Post{db: db}
}

func (r *Post) Create(ctx context.Context, p domain.Post, now time.Time) (domain.PostID, error) {
	topics := p.Pick.Topics
	if topics == nil {
		topics = []string{}
	}
	topicsJSON, err := json.Marshal(topics)
	if err != nil {
		return 0, fmt.Errorf("cannot encode topics of article %d: %w", p.ArticleID, err)
	}
	var id int64
	err = r.db.QueryRowContext(ctx, `
INSERT INTO post (article_id, run_id, channel_id, message_ts, description, topics, reason, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id`,
		p.ArticleID, p.RunID, p.ChannelID, p.MessageTS, p.Pick.Description, string(topicsJSON), p.Pick.Reason,
		formatTime(now), formatTime(now),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("cannot create post of article %d: %w", p.ArticleID, err)
	}
	return domain.PostID(id), nil
}

func (r *Post) FindByMessage(ctx context.Context, channelID, ts string) (domain.Post, error) {
	var (
		post       domain.Post
		topicsJSON string
	)
	err := r.db.QueryRowContext(ctx, `
SELECT p.id, p.article_id, a.source_id, p.run_id, p.channel_id, p.message_ts, a.url, p.description, p.topics, p.reason
FROM post p
JOIN article a ON a.id = p.article_id
WHERE p.channel_id = ? AND p.message_ts = ?`, channelID, ts,
	).Scan(&post.ID, &post.ArticleID, &post.SourceID, &post.RunID, &post.ChannelID, &post.MessageTS,
		&post.Pick.URL, &post.Pick.Description, &topicsJSON, &post.Pick.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Post{}, fmt.Errorf("post %s %s: %w", channelID, ts, domain.ErrNotFound)
	}
	if err != nil {
		return domain.Post{}, fmt.Errorf("cannot find post %s %s: %w", channelID, ts, err)
	}
	if err := json.Unmarshal([]byte(topicsJSON), &post.Pick.Topics); err != nil {
		return domain.Post{}, fmt.Errorf("cannot decode topics of post %d: %w", post.ID, err)
	}
	return post, nil
}
