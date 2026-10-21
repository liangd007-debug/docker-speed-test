package main

import "testing"

func TestParseMirrorLine(t *testing.T) {
	cases := map[string]string{
		"https://docker.1ms.run":                  "https://docker.1ms.run",
		"@url:`https://docker.1ms.run`":           "https://docker.1ms.run",
		"@url: https://docker.xuanyuan.me":        "https://docker.xuanyuan.me",
		"url:https://a.b":                         "https://a.b",
		"  'https://e.f',":                        "https://e.f",
		"https://g.h,":                            "https://g.h",
		"g.h":                                     "https://g.h",
		"http://plain.http":                       "http://plain.http",
		"# 注释":                                    "",
		"":                                        "",
		"   ":                                     "",
		"https://x.y # 尾部注释":                     "https://x.y",
		"@@@not-a-url":                            "",
		"https://ok.example.com/path/with?q=1":     "https://ok.example.com/path/with?q=1",
	}
	for in, want := range cases {
		got := parseMirrorLine(in)
		if got != want {
			t.Errorf("parseMirrorLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitImage(t *testing.T) {
	cases := map[string][2]string{
		"busybox:latest":     {"library/busybox", "latest"},
		"nginx":              {"library/nginx", "latest"},
		"nginx:1.25-alpine":  {"library/nginx", "1.25-alpine"},
		"bitnami/nginx:1.0":  {"bitnami/nginx", "1.0"},
		"":                   {"library/busybox", "latest"},
	}
	for in, want := range cases {
		repo, ref := splitImage(in)
		if repo != want[0] || ref != want[1] {
			t.Errorf("splitImage(%q) = (%q,%q), want (%q,%q)", in, repo, ref, want[0], want[1])
		}
	}
}

func TestAtof(t *testing.T) {
	if v, err := atof("10"); err != nil || v != 10 {
		t.Errorf("atof(10)=%v,%v", v, err)
	}
	if v, err := atof("0.5"); err != nil || v != 0.5 {
		t.Errorf("atof(0.5)=%v,%v", v, err)
	}
	if v, err := atof("1.25"); err != nil || v != 1.25 {
		t.Errorf("atof(1.25)=%v,%v", v, err)
	}
	if _, err := atof("abc"); err == nil {
		t.Error("atof(abc) should fail")
	}
}

func TestParseChallenge(t *testing.T) {
	c := parseChallenge(`Bearer realm="https://auth.example.com/token",service="registry.example.com",scope="repository:library/busybox:pull"`)
	if c.realm != "https://auth.example.com/token" || c.service != "registry.example.com" || c.scope != "repository:library/busybox:pull" || c.basic {
		t.Errorf("parseChallenge got %+v", c)
	}
	if c := parseChallenge("Basic realm=x"); !c.basic {
		t.Error("basic not detected")
	}
}
