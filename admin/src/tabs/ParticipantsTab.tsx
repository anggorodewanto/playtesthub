/**
 * ParticipantsTab — placeholder shipped in C4, filled in C6 with the
 * 6-column table per docs/STATUS_M5.md C6.
 */

import { Alert, Button, Space, Typography } from 'antd'
import { useNavigate } from 'react-router'
import type { V1Playtest } from '../playtesthubapi/generated-definitions/V1Playtest'

export function ParticipantsTab({ playtest }: { playtest: V1Playtest }) {
  const navigate = useNavigate()
  return (
    <Space direction="vertical" style={{ width: '100%' }} data-testid="participants-tab">
      <Typography.Text>Participants surface lands in M5.C phase 6.</Typography.Text>
      <Alert
        type="info"
        showIcon
        message="Use the legacy applicants page until the new 6-column table replaces it."
      />
      <Button onClick={() => navigate(`/${playtest.id ?? ''}/applicants`)}>Open applicants page</Button>
    </Space>
  )
}
