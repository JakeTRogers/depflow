// Tests for repository policies and selected merge arguments.
package githubcli

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestMergeMethods(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"merge", "squash", "rebase", "", "--admin"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			exec := &stubExecutor{}
			err := newClient(exec).MergePullRequest(context.Background(), "git.example.com/acme/tool", 42, true, method, "")
			valid := method == "merge" || method == "squash" || method == "rebase"
			if !valid {
				if err == nil || len(exec.calls) != 0 {
					t.Fatalf("invalid method: %v, %v", err, exec.calls)
				}
				return
			}
			want := []string{"pr", "merge", "42", "--" + method, "--delete-branch", "--admin", "--repo", "git.example.com/acme/tool"}
			if err != nil || !reflect.DeepEqual(exec.calls, [][]string{want}) {
				t.Fatalf("%v, %v", exec.calls, err)
			}
		})
	}
}

func TestReadMergeCapabilities(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		data string
		want []string
		fail bool
	}{
		{`{"url":"https://git.example.com/acme/tool","mergeCommitAllowed":false,"squashMergeAllowed":true,"rebaseMergeAllowed":true}`, []string{"squash", "rebase"}, false},
		{`{"url":"https://github.com/acme/tool","mergeCommitAllowed":false,"squashMergeAllowed":false,"rebaseMergeAllowed":false}`, nil, false},
		{`{"url":"https://github.com/acme/tool","squashMergeAllowed":true}`, nil, true},
		{`{}`, nil, true}, {`broken`, nil, true},
	} {
		exec := &stubExecutor{output: []byte(test.data)}
		caps, err := newClient(exec).ReadMergeCapabilities(context.Background(), "git.example.com/acme/tool")
		if (err != nil) != test.fail || !reflect.DeepEqual(caps.Methods, test.want) {
			t.Fatalf("%s: %+v, %v", test.data, caps, err)
		}
		if !test.fail {
			err := caps.Require("merge")
			var conflict *MethodNotAllowedError
			if !errors.As(err, &conflict) {
				t.Fatalf("Require: %v", err)
			}
		}
	}
}

func TestCheckMergeAllowed(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		queue  string
		method string
		want   string
	}{
		{"false", "squash", ""}, {"false", "merge", "disabled"}, {"true", "squash", "merge-queue"}, {"null", "squash", "queue status"},
	} {
		data := `{"data":{"repository":{"url":"https://git.example.com/acme/tool","mergeCommitAllowed":false,"squashMergeAllowed":true,"rebaseMergeAllowed":true,"pullRequest":{"isMergeQueueEnabled":` + test.queue + `}}}}`
		exec := &stubExecutor{output: []byte(data)}
		err := newClient(exec).CheckMergeAllowed(context.Background(), "git.example.com/acme/tool", 42, test.method)
		if test.want == "" && err != nil || test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
			t.Fatalf("%+v: %v", test, err)
		}
		args := strings.Join(exec.calls[0], " ")
		if !strings.Contains(args, "--hostname git.example.com") || !strings.Contains(args, "number=42") {
			t.Fatal(args)
		}
	}
}
