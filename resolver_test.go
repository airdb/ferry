package main

import (
	"context"
	"net"
	"testing"
)

func TestDoHResolver(t *testing.T) {
	cases := []struct {
		Host string
		IP   string
	}{
		{"192-168-2-1.nip.io", "192.168.2.1"},
		{"forcesafesearch.google.com", "216.239.38.120"},
	}

	r := &net.Resolver{
		PreferGo: true,
		Dial: (&DoHResolverDialer{
			EndPoint:  "https://dns.google/dns-query",
			UserAgent: DefaultUserAgent,
		}).DialContext,
	}

	for _, c := range cases {
		v, err := r.LookupHost(context.TODO(), c.Host)
		// t.Logf("LookupAddr(%#v) return v=%+v err=%+v", c.Host, v, err)
		if err != nil || v[0] != c.IP {
			t.Errorf("LookupAddr(%#v) must return %#v, not %+v, err=%+v", c.Host, c.IP, v, err)
		}
	}
}

func TestRegionResolver_LookupCity(t *testing.T) {
	r := &RegionResolver{Resolver: nil}
	r.Load("testdata/*.mmdb")

	type args struct {
		ctx context.Context
		ip  net.IP
	}
	type testcase struct {
		name    string
		args    args
		want    string
		want1   string
		want2   string
		wantErr bool
	}
	tests := []testcase{
		{"nodb", args{}, "", "", "", true},
	}
	if db := r.GetCityDB(); db != nil {
		tests = append(tests,
			testcase{"city", args{
				ctx: context.TODO(), ip: net.ParseIP("101.80.46.0"),
			}, "CN", "Shanghai", "Shanghai", false},
			testcase{"city", args{
				ctx: context.TODO(), ip: net.ParseIP("36.27.0.0"),
			}, "CN", "Zhejiang", "Hangzhou", false},
		)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, got2, err := r.LookupCity(tt.args.ctx, tt.args.ip)
			if (err != nil) != tt.wantErr {
				t.Errorf("RegionResolver.LookupCity() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("RegionResolver.LookupCity() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("RegionResolver.LookupCity() got1 = %v, want %v", got1, tt.want1)
			}
			if got2 != tt.want2 {
				t.Errorf("RegionResolver.LookupCity() got2 = %v, want %v", got2, tt.want2)
			}
		})
	}
}

func BenchmarkRegionResolver_LookupCity(b *testing.B) {
	r := &RegionResolver{Resolver: nil}
	r.Load("testdata/*.mmdb")

	ctx := context.TODO()
	for i := 0; i < b.N; i++ {
		r.LookupCity(ctx, net.ParseIP("36.27.0.0"))
	}
}

func BenchmarkIsBogusChinaIP(b *testing.B) {
	for i := 0; i < b.N; i++ {
		IsBogusChinaIP(net.ParseIP("36.27.0.0"))
	}
}
