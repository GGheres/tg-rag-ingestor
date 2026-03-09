package telegram

import "time"

func MessageURL(channelUsername string, messageID int64) string {
	return "https://t.me/" + channelUsername + "/" + itoa64(messageID)
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

func itoa64(v int64) string {
	if v == 0 {
		return "0"
	}
	negative := v < 0
	if negative {
		v = -v
	}
	buf := make([]byte, 0, 20)
	for v > 0 {
		buf = append(buf, byte('0'+(v%10)))
		v /= 10
	}
	if negative {
		buf = append(buf, '-')
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}
