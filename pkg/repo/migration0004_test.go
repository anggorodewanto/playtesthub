package repo_test

import (
	"context"
	"testing"
)

// TestMigration0004_ApplicantDiscordUserIDColumn pins the shape of
// applicant.discord_user_id added by migration 0004: TEXT, nullable.
// The DM worker treats NULL as `lastDmError='missing_recipient'`
// (docs/errors.md, docs/dm-queue.md).
func TestMigration0004_ApplicantDiscordUserIDColumn(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	const sql = `
		SELECT data_type, is_nullable
		  FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND table_name = 'applicant'
		   AND column_name = 'discord_user_id'`

	var dataType, isNullable string
	if err := testPool.QueryRow(ctx, sql).Scan(&dataType, &isNullable); err != nil {
		t.Fatalf("query column: %v", err)
	}
	if dataType != "text" {
		t.Errorf("data_type = %q, want text", dataType)
	}
	if isNullable != "YES" {
		t.Errorf("is_nullable = %q, want YES", isNullable)
	}
}
