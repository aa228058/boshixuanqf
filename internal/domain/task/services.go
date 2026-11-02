package task

import (
	"regexp"
	"strconv"
	"strings"
)

func isGlobalFlood(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "flood_wait") && !strings.Contains(errStr, "slowmode_wait") ||
		strings.Contains(errStr, "peer_flood") ||
		strings.Contains(errStr, "flood_test_phone_wait")
}

// isGroupLevelRestriction checks if error is group-level (does not affect account status)

func isGroupLevelRestriction(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "chat_write_forbidden") ||
		strings.Contains(errStr, "user_is_blocked") ||
		strings.Contains(errStr, "slowmode_wait")
}

// isAccountBanned checks if error indicates the account itself is banned/deactivated

func isAccountBanned(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "phone_number_banned") ||
		strings.Contains(errStr, "user_deactivated") ||
		strings.Contains(errStr, "auth_key_unregistered")
}

// floodWaitRe is compiled once at package init to avoid recompilation on every error.
var floodWaitRe = regexp.MustCompile(`flood_wait_(\d+)`)

// extractFloodSeconds extracts wait seconds from FLOOD_WAIT error string.

func extractFloodSeconds(errStr string) int {
	matches := floodWaitRe.FindStringSubmatch(strings.ToLower(errStr))
	if len(matches) > 1 {
		if sec, err := strconv.Atoi(matches[1]); err == nil {
			return sec
		}
	}
	return 0
}

// markAccountRestricted records a risk event AND updates account status to restricted.

func isInviteRequestSent(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "join_request") ||
		strings.Contains(errStr, "request_to_join") ||
		strings.Contains(errStr, "invite_request") ||
		strings.Contains(errStr, "need_admin_approval") ||
		strings.Contains(errStr, "channel_private") ||
		strings.Contains(errStr, "chat_private")
}

// isAlreadyMember checks if the user is already a member of the group/channel.
// Telegram returns ALREADY_PARTICIPANT or USER_ALREADY_PARTICIPANT when trying to join again.

func isAlreadyMember(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "already_participant") ||
		strings.Contains(errStr, "user_already") ||
		strings.Contains(errStr, "already_in_chat")
}

// P2-13 审计清理: notifyQuotaExceeded 只有注释无函数体(从未实现, 也未被调用),
// 占位注释删除。软配额提醒将来实现时重新添加。

func extractInviteHash(link string) string {
	link = strings.TrimSpace(link)
	if idx := strings.LastIndex(link, "+"); idx != -1 {
		return link[idx+1:]
	}
	if idx := strings.LastIndex(link, "/"); idx != -1 {
		return link[idx+1:]
	}
	return link
}

// StartApprovalChecker starts a background goroutine to check pending join approvals.
