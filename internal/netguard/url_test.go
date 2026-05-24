package netguard

import "testing"

func TestValidatePublicHTTPSBaseURLRejectsUnsafeTargets(t *testing.T) {
	cases := []string{
		"http://example.com",
		"file:///etc/passwd",
		"https://127.0.0.1",
		"https://10.0.0.1",
		"https://172.16.0.1",
		"https://192.168.0.1",
		"https://169.254.169.254",
		"https://[::1]",
		"https://user@example.com",
	}
	for _, input := range cases {
		if err := ValidatePublicHTTPSBaseURL(input); err == nil {
			t.Fatalf("expected %s to be rejected", input)
		}
	}
}

func TestValidatePublicHTTPSBaseURLAllowsPublicHTTPSIP(t *testing.T) {
	if err := ValidatePublicHTTPSBaseURL("https://8.8.8.8"); err != nil {
		t.Fatalf("expected public https ip to be allowed: %v", err)
	}
}
