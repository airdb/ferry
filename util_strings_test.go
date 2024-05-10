package main

import (
	"strings"
	"testing"
)

func BenchmarkStdLower(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = strings.ToLower("The Quick Brown Fox Jumps Over The Lazy Dog")
	}
}

func BenchmarkFastLower(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ToLower("The Quick Brown Fox Jumps Over The Lazy Dog")
	}
}

func BenchmarkFastUpper(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ToUpper("The Quick Brown Fox Jumps Over The Lazy Dog")
	}
}
