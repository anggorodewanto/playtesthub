/**
 * DiscordBotToolsTab — placeholder shipped in C4, filled in C7 with the
 * Create Announcement form + history table per docs/STATUS_M5.md C7.
 */

import { Alert, Space, Typography } from 'antd'
import type { V1Playtest } from '../playtesthubapi/generated-definitions/V1Playtest'

export function DiscordBotToolsTab({ playtest }: { playtest: V1Playtest }) {
  return (
    <Space direction="vertical" style={{ width: '100%' }} data-testid="bot-tools-tab">
      <Typography.Text>Bulk Discord broadcast tooling lands in M5.C phase 7.</Typography.Text>
      <Alert
        type="info"
        showIcon
        message={`Playtest: ${playtest.title ?? playtest.slug ?? '—'} — placeholder. The Send via DM form + announcement history land in C7.`}
      />
    </Space>
  )
}
