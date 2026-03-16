// post_type_test.go - Tests for social post type classification
package social

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/core/protocol"
)

func TestGetPostType(t *testing.T) {
	tests := []struct {
		name string
		msg  *protocol.Message
		want PostType
	}{
		{
			name: "post type",
			msg:  &protocol.Message{Header: protocol.Header{Ext: "social", Fields: map[string]string{"type": "post"}}},
			want: PostTypePost,
		},
		{
			name: "comment type",
			msg:  &protocol.Message{Header: protocol.Header{Ext: "social", Fields: map[string]string{"type": "comment"}}},
			want: PostTypeComment,
		},
		{
			name: "repost type",
			msg:  &protocol.Message{Header: protocol.Header{Ext: "social", Fields: map[string]string{"type": "repost"}}},
			want: PostTypeRepost,
		},
		{
			name: "quote type",
			msg:  &protocol.Message{Header: protocol.Header{Ext: "social", Fields: map[string]string{"type": "quote"}}},
			want: PostTypeQuote,
		},
		{
			name: "nil message defaults to post",
			msg:  nil,
			want: PostTypePost,
		},
		{
			name: "non-social extension defaults to post",
			msg:  &protocol.Message{Header: protocol.Header{Ext: "pm", Fields: map[string]string{"type": "issue"}}},
			want: PostTypePost,
		},
		{
			name: "unknown type defaults to post",
			msg:  &protocol.Message{Header: protocol.Header{Ext: "social", Fields: map[string]string{"type": "unknown"}}},
			want: PostTypePost,
		},
		{
			name: "missing type defaults to post",
			msg:  &protocol.Message{Header: protocol.Header{Ext: "social", Fields: map[string]string{}}},
			want: PostTypePost,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetPostType(tt.msg)
			if got != tt.want {
				t.Errorf("GetPostType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsEmptyRepost(t *testing.T) {
	tests := []struct {
		name string
		msg  *protocol.Message
		want bool
	}{
		{
			name: "empty content repost",
			msg: &protocol.Message{
				Content: "",
				Header:  protocol.Header{Ext: "social", Fields: map[string]string{"type": "repost"}},
			},
			want: true,
		},
		{
			name: "hashtag-only repost",
			msg: &protocol.Message{
				Content: "#topic",
				Header:  protocol.Header{Ext: "social", Fields: map[string]string{"type": "repost"}},
			},
			want: true,
		},
		{
			name: "hashtag with newline repost",
			msg: &protocol.Message{
				Content: "#topic\n",
				Header:  protocol.Header{Ext: "social", Fields: map[string]string{"type": "repost"}},
			},
			want: true,
		},
		{
			name: "repost with content",
			msg: &protocol.Message{
				Content: "Great post!",
				Header:  protocol.Header{Ext: "social", Fields: map[string]string{"type": "repost"}},
			},
			want: false,
		},
		{
			name: "nil message",
			msg:  nil,
			want: false,
		},
		{
			name: "not a repost",
			msg: &protocol.Message{
				Content: "",
				Header:  protocol.Header{Ext: "social", Fields: map[string]string{"type": "post"}},
			},
			want: false,
		},
		{
			name: "non-social repost",
			msg: &protocol.Message{
				Content: "",
				Header:  protocol.Header{Ext: "pm", Fields: map[string]string{"type": "repost"}},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsEmptyRepost(tt.msg)
			if got != tt.want {
				t.Errorf("IsEmptyRepost() = %v, want %v", got, tt.want)
			}
		})
	}
}
