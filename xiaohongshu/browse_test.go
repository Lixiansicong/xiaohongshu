package xiaohongshu

import (
	"testing"
)

func TestParseNoteURL(t *testing.T) {
	tests := []struct {
		name          string
		url           string
		expectedFeedID string
		expectedToken  string
	}{
		{
			name:          "完整 URL",
			url:           "/explore/68e66fef0000000004023fdb?xsec_token=ABc9MCVTGMXqvxLT8H-fHb_6DodO8iEoHByoltzPex20I=&xsec_source=",
			expectedFeedID: "68e66fef0000000004023fdb",
			expectedToken:  "ABc9MCVTGMXqvxLT8H-fHb_6DodO8iEoHByoltzPex20I=",
		},
		{
			name:          "带 pc_feed 的 URL",
			url:           "/explore/68ebe520000000000702039c?xsec_token=ABrYg9Jn28WjYaI1Kj4cUtUTQnwSJB92pzKDI8V_47CIo=&xsec_source=pc_feed",
			expectedFeedID: "68ebe520000000000702039c",
			expectedToken:  "ABrYg9Jn28WjYaI1Kj4cUtUTQnwSJB92pzKDI8V_47CIo=",
		},
		{
			name:          "另一个完整 URL",
			url:           "/explore/68ea423d0000000004013ff3?xsec_token=ABVGNDRZ66j_hybhC_ySCokwCW2Vu6j_fk4Wsic8FFdQc=&xsec_source=pc_feed",
			expectedFeedID: "68ea423d0000000004013ff3",
			expectedToken:  "ABVGNDRZ66j_hybhC_ySCokwCW2Vu6j_fk4Wsic8FFdQc=",
		},
		{
			name:          "没有查询参数的 URL",
			url:           "/explore/68e495f20000000004014d47",
			expectedFeedID: "68e495f20000000004014d47",
			expectedToken:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			feedID, token := parseNoteURL(tt.url)
			
			if feedID != tt.expectedFeedID {
				t.Errorf("feedID 解析错误，期望: %s, 实际: %s", tt.expectedFeedID, feedID)
			}
			
			if token != tt.expectedToken {
				t.Errorf("xsecToken 解析错误，期望: %s, 实际: %s", tt.expectedToken, token)
			}
		})
	}
}
