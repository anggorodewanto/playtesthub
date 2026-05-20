/**
 * DistributionTab — placeholder shipped in C4, filled in C5.
 * The full per-model rendering (ADT / STEAM_KEYS / AGS_CAMPAIGN) lands
 * in M5.C phase 5; for now we link out to the existing CodePoolPage so
 * the operator can still manage codes from inside the detail page.
 */

import { Alert, Button, Space, Typography } from 'antd'
import { useNavigate } from 'react-router'
import type { V1Playtest } from '../playtesthubapi/generated-definitions/V1Playtest'

export function DistributionTab({ playtest }: { playtest: V1Playtest }) {
  const navigate = useNavigate()
  return (
    <Space direction="vertical" style={{ width: '100%' }} data-testid="distribution-tab">
      <Typography.Text strong>Distribution model</Typography.Text>
      <Typography.Text>{playtest.distributionModel ?? '—'}</Typography.Text>
      <Alert
        type="info"
        showIcon
        message="Code pool affordances live on the legacy page (M5.C phase 5 inlines them here)."
      />
      <Button onClick={() => navigate(`/${playtest.id ?? ''}/codes`)}>Open code pool page</Button>
    </Space>
  )
}
