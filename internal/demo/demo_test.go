package demo

import (
	"context"
	"testing"
	"time"

	"github.com/projector22/vrotrss/internal/greader"
)

var ctx = context.Background()

func TestStreamPagingAndFilters(t *testing.T) {
	c := New(time.Now())
	p1, err := c.Stream(ctx, greader.StreamQuery{Count: 30})
	if err != nil || len(p1.Items) != 30 || p1.Continuation != "30" {
		t.Fatalf("page1: %d items, cont %q, err %v", len(p1.Items), p1.Continuation, err)
	}
	p2, _ := c.Stream(ctx, greader.StreamQuery{Count: 30, Continuation: p1.Continuation})
	p3, _ := c.Stream(ctx, greader.StreamQuery{Count: 30, Continuation: p2.Continuation})
	if len(p1.Items)+len(p2.Items)+len(p3.Items) != itemCount || p3.Continuation != "" {
		t.Errorf("paging lost items: %d %d %d", len(p1.Items), len(p2.Items), len(p3.Items))
	}
	for i := 1; i < len(p1.Items); i++ {
		if p1.Items[i].Published.After(p1.Items[i-1].Published) {
			t.Fatal("items must be newest first")
		}
	}
	old, _ := c.Stream(ctx, greader.StreamQuery{Oldest: true, Count: 1})
	if old.Items[0].ID != "tag:google.com,2005:reader/item/000000000000003f" {
		t.Errorf("oldest = %s", old.Items[0].ID)
	}
	if _, err := c.Stream(ctx, greader.StreamQuery{Continuation: "x"}); err == nil {
		t.Error("bad continuation should fail")
	}
	end, _ := c.Stream(ctx, greader.StreamQuery{Continuation: "9999"})
	if len(end.Items) != 0 {
		t.Error("continuation past the end should be empty")
	}
}

func TestMutationsAndCounts(t *testing.T) {
	c := New(time.Now())
	before, _ := c.UnreadCounts(ctx)
	id := "tag:google.com,2005:reader/item/0000000000000001"
	_ = c.SetRead(ctx, []string{id}, true)
	_ = c.SetStarred(ctx, []string{id}, false)
	after, _ := c.UnreadCounts(ctx)
	if after[greader.StreamReadingList] != before[greader.StreamReadingList]-1 || after["feed/1"] != before["feed/1"]-1 {
		t.Errorf("counts before=%v after=%v", before, after)
	}
	if it, ok := c.Item(id); !ok || !it.Read || it.Starred {
		t.Errorf("item = %+v ok=%v", it, ok)
	}
	if _, ok := c.Item("nope"); ok {
		t.Error("unknown item found")
	}
	if err := c.MarkAllRead(ctx, "user/-/label/Tech", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	counts, _ := c.UnreadCounts(ctx)
	if counts["user/-/label/Tech"] != 0 || counts["user/-/label/News"] == 0 {
		t.Errorf("mark-all must be scoped to the stream: %v", counts)
	}
	subs, _ := c.Subscriptions(ctx)
	if len(subs) != feedCount {
		t.Errorf("subs = %d", len(subs))
	}
}
