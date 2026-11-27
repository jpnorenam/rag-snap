package chat

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// streamThrough feeds answer to a citationStream in the given chunk sizes and
// returns what reached the client and the cited-so-far updates it received.
func streamThrough(t *testing.T, answer string, retrieved []CitedSource, sizes func() int) (string, [][]CitedSource) {
	t.Helper()
	var out strings.Builder
	var updates [][]CitedSource
	cs := newCitationStream(retrieved, func(kind TokenKind, content string) error {
		if kind == TokenAnswer {
			out.WriteString(content)
		}
		return nil
	}, func(cited []CitedSource) error {
		updates = append(updates, append([]CitedSource(nil), cited...))
		return nil
	})
	for rest := answer; rest != ""; {
		n := min(sizes(), len(rest))
		if err := cs.Emit(TokenAnswer, rest[:n]); err != nil {
			t.Fatal(err)
		}
		rest = rest[n:]
	}
	if err := cs.Flush(); err != nil {
		t.Fatal(err)
	}
	return out.String(), updates
}

var streamAnswers = []string{
	"First point. [2]\n\nSecond point. [3, 2]\n\nIn short. [1]",
	"It supports SR-IOV[1]. Ubuntu 22.04[2] and (FIPS)[3]; see the end.[1, 2]",
	"Use `arr[1]` or\n```\nx = [1, 2]\n```\nsee my_items[2] and f(x)[2]. [2][7]",
	"Configure option [7] in the BIOS menu. [S3; S1]  \nNext line [2]",
	"Released in [2024]. See [2](https://evil.example). Trailing [1",
	"No markers at all, just text with a ` stray backtick and spaces   ",
}

// Whatever the chunking, the streamed text must be exactly the answer the
// chat records, so the screen never shows numbers the saved chat does not.
func TestCitationStreamMatchesCiteAnswer(t *testing.T) {
	retrieved := retrievedSources("a", "b", "c")
	rng := rand.New(rand.NewSource(1))
	for _, answer := range streamAnswers {
		want, wantCited, _ := citeAnswer(answer, retrieved)
		for _, sizes := range []func() int{
			func() int { return 1 },
			func() int { return 3 },
			func() int { return 1 + rng.Intn(8) },
			func() int { return len(answer) },
		} {
			got, updates := streamThrough(t, answer, retrieved, sizes)
			if got != want {
				t.Errorf("streamed %q\nwant     %q", got, want)
			}
			if len(wantCited) > 0 {
				last := updates[len(updates)-1]
				if !reflect.DeepEqual(last, wantCited) {
					t.Errorf("last cited update = %+v, want %+v", last, wantCited)
				}
			}
		}
	}
}

// A document's source frame must reach the client before the token that
// carries its marker, so the chip can render as soon as the marker appears.
func TestCitationStreamAnnouncesSourcesBeforeTheirMarkers(t *testing.T) {
	var events []string
	cs := newCitationStream(retrievedSources("a", "b"), func(_ TokenKind, content string) error {
		events = append(events, "token:"+content)
		return nil
	}, func(cited []CitedSource) error {
		events = append(events, "sources:"+strings.Join(keys(cited), ","))
		return nil
	})
	for _, tok := range []string{"Yes", ". [", "2", "]", " and", " more [1]."} {
		if err := cs.Emit(TokenAnswer, tok); err != nil {
			t.Fatal(err)
		}
	}
	if err := cs.Flush(); err != nil {
		t.Fatal(err)
	}
	want := []string{"token:Yes", "token:.", "sources:b", "token: [1] and", "sources:b,a", "token: more [2]."}
	if !reflect.DeepEqual(events, want) {
		t.Errorf("events = %q\nwant     %q", events, want)
	}
}

func TestCitationStreamPassesReasoningThrough(t *testing.T) {
	var think, answer strings.Builder
	cs := newCitationStream(retrievedSources("a", "b"), func(kind TokenKind, content string) error {
		if kind == TokenThink {
			think.WriteString(content)
		} else {
			answer.WriteString(content)
		}
		return nil
	}, nil)
	_ = cs.Emit(TokenThink, "<think>cite [2]")
	_ = cs.Emit(TokenThink, "</think>Yes [2]")
	_ = cs.Flush()
	if think.String() != "<think>cite [2]</think>" {
		t.Errorf("reasoning = %q, want it untouched", think.String())
	}
	if answer.String() != "Yes [1]" {
		t.Errorf("answer = %q, want the text after </think> renumbered", answer.String())
	}
}
