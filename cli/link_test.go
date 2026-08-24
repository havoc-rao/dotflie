package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostTagNorm(t *testing.T) {
	cases := []struct{ in, want string }{
		{"macbook-pro", "macbook-pro"},
		{"MacBook-Pro.local", "macbook-pro"},
		{"  VM-222-213 ", "vm-222-213"},
		{"vm.local.local", "vm.local"}, // 只去掉一个 .local 后缀
	}
	for _, tc := range cases {
		if got := hostTagNorm(tc.in); got != tc.want {
			t.Errorf("hostTagNorm(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMatchHost(t *testing.T) {
	cases := []struct {
		host string
		pats []string
		want bool
	}{
		{"macbook-pro", []string{"macbook-pro"}, true},
		{"MacBook-Pro.local", []string{"macbook-pro"}, true},
		{"macbook-pro", []string{" macbook-pro "}, true},
		{"vm-222-213", []string{"macbook-pro"}, false},
		{"macbook", []string{"macbook-pro"}, false}, // 精确匹配,非子串
	}
	for _, tc := range cases {
		if got := matchHost(tc.host, tc.pats); got != tc.want {
			t.Errorf("matchHost(%q, %v) = %v, want %v", tc.host, tc.pats, got, tc.want)
		}
	}
}

func TestLinkAppliesToHost(t *testing.T) {
	cases := []struct {
		name string
		l    LinkSpec
		host string
		want bool
	}{
		{"no filter", LinkSpec{Src: "a", Dest: "~/.a"}, "any-host", true},
		{"only hit", LinkSpec{Src: "a", Dest: "~/.a", Only: []string{"MacBook-Pro"}}, "macbook-pro.local", true},
		{"only miss", LinkSpec{Src: "a", Dest: "~/.a", Only: []string{"macbook-pro"}}, "vm-222-213", false},
		{"except hit", LinkSpec{Src: "a", Dest: "~/.a", Except: []string{"VM-222-213"}}, "vm-222-213", false},
		{"except miss", LinkSpec{Src: "a", Dest: "~/.a", Except: []string{"vm-222-213"}}, "macbook-pro", true},
	}
	for _, tc := range cases {
		got, err := tc.l.LinkAppliesToHost(tc.host)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: want %v, got %v", tc.name, tc.want, got)
		}
	}
}

func TestLinkOnlyAndExceptConflict(t *testing.T) {
	l := LinkSpec{Src: "x", Dest: "~/.x", Only: []string{"a"}, Except: []string{"b"}}
	if _, err := l.LinkAppliesToHost("anything"); err == nil {
		t.Fatal("want error when both only and except are set")
	}
}

func TestCollectLazyResolveAndFilter(t *testing.T) {
	root := t.TempDir()
	m := &Manifest{
		Links: []LinkSpec{
			{Src: "zsh/.zshrc", Dest: "~/.zshrc"},
			{Src: "proj/rules", Dest: "{unset_key}/rules"},
			{Src: "mac-only", Dest: "~/.mac-only", Only: []string{"this-host-never-exists"}},
			{Src: "skip-me", Dest: "~/.skip-me", Except: []string{HostTag()}},
		},
	}

	// 全量:未设置 ref 的条目以 ref-unset 呈现,不中断;only/except 过滤生效
	entries, err := Collect(m, root, nil)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries (zsh, proj), got %d: %+v", len(entries), entries)
	}
	bySrc := map[string]Entry{}
	for _, e := range entries {
		bySrc[e.Link.Src] = e
	}
	if e := bySrc["proj/rules"]; e.ResolveErr == nil || e.Status != StatusRefUnset {
		t.Errorf("proj/rules: want ref-unset with error, got status=%v err=%v", e.Status, e.ResolveErr)
	} else if !strings.Contains(e.ResolveErr.Error(), "unset_key") {
		t.Errorf("proj/rules: error should mention the unset key, got %v", e.ResolveErr)
	}
	if e := bySrc["zsh/.zshrc"]; e.ResolveErr != nil || e.Status != StatusMissingSrc {
		t.Errorf("zsh/.zshrc: want resolved missing-src, got status=%v err=%v", e.Status, e.ResolveErr)
	}
	if _, ok := bySrc["mac-only"]; ok {
		t.Error("mac-only should be filtered out by only")
	}
	if _, ok := bySrc["skip-me"]; ok {
		t.Error("skip-me should be filtered out by except")
	}

	// 定向:只取 zsh 条目,即使 proj/rules 有未设置 ref 也不报错
	entries, err = Collect(m, root, []string{"zsh/.zshrc"})
	if err != nil {
		t.Fatalf("Collect targeted: %v", err)
	}
	if len(entries) != 1 || entries[0].Link.Src != "zsh/.zshrc" {
		t.Fatalf("targeted collect: want only zsh/.zshrc, got %+v", entries)
	}

	// 定向被过滤的条目:应为空
	entries, err = Collect(m, root, []string{"mac-only"})
	if err != nil {
		t.Fatalf("Collect excluded target: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("excluded target should yield no entries, got %+v", entries)
	}
}

func TestCollectOnlyExceptConflict(t *testing.T) {
	m := &Manifest{Links: []LinkSpec{
		{Src: "x", Dest: "~/.x", Only: []string{"a"}, Except: []string{"b"}},
	}}
	if _, err := Collect(m, t.TempDir(), nil); err == nil {
		t.Fatal("want error when a link sets both only and except")
	}
}

// TestApplyLinksIgnoresRefUnsetInBulk: 全量模式 ref-unset 条目被忽略(不报错),
// 显式指定目标时按条报错提示。
func TestApplyLinksIgnoresRefUnsetInBulk(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", ".zshrc"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manifest{Links: []LinkSpec{
		{Src: "a/.zshrc", Dest: filepath.Join(root, "out", ".zshrc")},
		{Src: "bad", Dest: "{unset}/x"},
	}}
	entries, err := Collect(m, root, nil)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	// 全量(非严格):ref-unset 被忽略,不报错
	if err := applyLinks(entries, Options{}, false, false); err != nil {
		t.Fatalf("bulk applyLinks should ignore ref-unset, got error: %v", err)
	}
	// 显式目标(严格):ref-unset 报错
	if err := applyLinks(entries, Options{}, false, true); err == nil {
		t.Fatal("strict applyLinks should error on ref-unset entry")
	}
}
