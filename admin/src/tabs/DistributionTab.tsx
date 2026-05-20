/**
 * DistributionTab — M5.C phase 5 / docs/STATUS_M5.md D6.
 *
 * Dispatches on playtest.distributionModel and renders the per-model
 * shape with a shared empty-state scaffold. The legacy CodePoolPage is
 * still reachable via the "Open code pool page" affordance for the
 * affordances that haven't been moved here yet (CSV upload UI, sync
 * action) — those stay on the legacy page during the M5.C transition.
 */

import { useAppUIContext } from '@accelbyte/sdk-extend-app-ui'
import { Alert, Button, Card, Space, Statistic, Tag, Typography } from 'antd'
import { useNavigate } from 'react-router'
import type { V1Playtest } from '../playtesthubapi/generated-definitions/V1Playtest'
import { usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId } from '../playtesthubapi/generated-admin/queries/PlaytesthubServiceAdmin.query'

const DistributionModel = {
  STEAM_KEYS: 'DISTRIBUTION_MODEL_STEAM_KEYS',
  AGS_CAMPAIGN: 'DISTRIBUTION_MODEL_AGS_CAMPAIGN',
  ADT: 'DISTRIBUTION_MODEL_ADT'
}

export function DistributionTab({ playtest }: { playtest: V1Playtest }) {
  const model = playtest.distributionModel ?? ''
  switch (model) {
    case DistributionModel.ADT:
      return <ADTPanel playtest={playtest} />
    case DistributionModel.STEAM_KEYS:
      return <SteamKeysPanel playtest={playtest} />
    case DistributionModel.AGS_CAMPAIGN:
      return <AGSCampaignPanel playtest={playtest} />
    default:
      return (
        <Alert
          type="info"
          showIcon
          message={`Distribution model: ${model || 'unspecified'}`}
          description="No distribution-specific UI is available for this model."
          data-testid="distribution-tab"
        />
      )
  }
}

function ADTPanel({ playtest }: { playtest: V1Playtest }) {
  const linked = Boolean(playtest.adtNamespace)
  return (
    <Space direction="vertical" style={{ width: '100%' }} data-testid="distribution-tab">
      <Typography.Title level={4} style={{ marginTop: 0 }}>
        ADT distribution
      </Typography.Title>
      {!linked ? (
        <Card>
          <Space direction="vertical">
            <Typography.Text strong>🔗 ADT Namespace Not Linked</Typography.Text>
            <Typography.Text type="secondary">
              Link your studio's ADT namespace to surface builds and approve players against this playtest.
            </Typography.Text>
            <Typography.Text type="secondary">
              Linking happens from the Playtests list page → Link new ADT Namespace.
            </Typography.Text>
          </Space>
        </Card>
      ) : (
        <Card>
          <Space direction="vertical" size="small">
            <Tag color="blue">ADT linkage is studio-wide</Tag>
            <FieldRow label="ADT Namespace" value={playtest.adtNamespace ?? '—'} />
            <FieldRow label="Game ID" value={playtest.adtGameId ?? '—'} />
            <FieldRow label="Build ID" value={playtest.adtBuildId ?? '—'} />
            <FieldRow label="Fallback URL" value={playtest.adtFallbackDownloadUrl ?? '(none)'} />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              ADT linkage is studio-wide; the same ADT namespace covers every playtest under this studio.
            </Typography.Text>
          </Space>
        </Card>
      )}
    </Space>
  )
}

function SteamKeysPanel({ playtest }: { playtest: V1Playtest }) {
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const { data, isLoading } = usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId(sdk, {
    playtestId: playtest.id ?? ''
  })
  const stats = data?.stats
  const total = stats?.total ?? 0
  const remaining = stats?.unused ?? 0
  const granted = stats?.granted ?? 0
  return (
    <Space direction="vertical" style={{ width: '100%' }} data-testid="distribution-tab">
      <Typography.Title level={4} style={{ marginTop: 0 }}>
        Steam keys
      </Typography.Title>
      {!isLoading && total === 0 ? (
        <Card>
          <Space direction="vertical">
            <Typography.Text strong>No codes uploaded yet</Typography.Text>
            <Typography.Text type="secondary">
              Upload a CSV of Steam keys to start approving applicants.
            </Typography.Text>
            <Button type="primary" onClick={() => navigate(`/${playtest.id ?? ''}/codes`)}>
              Upload Codes (CSV)
            </Button>
          </Space>
        </Card>
      ) : (
        <Space wrap>
          <Statistic title="Remaining" value={remaining} />
          <Statistic title="Granted" value={granted} />
          <Statistic title="Total" value={total} />
          <Button onClick={() => navigate(`/${playtest.id ?? ''}/codes`)}>Open code pool page</Button>
        </Space>
      )}
    </Space>
  )
}

function AGSCampaignPanel({ playtest }: { playtest: V1Playtest }) {
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const { data, isLoading } = usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId(sdk, {
    playtestId: playtest.id ?? ''
  })
  const stats = data?.stats
  return (
    <Space direction="vertical" style={{ width: '100%' }} data-testid="distribution-tab">
      <Typography.Title level={4} style={{ marginTop: 0 }}>
        AGS Campaign codes
      </Typography.Title>
      <Card>
        <Space direction="vertical" size="small">
          <FieldRow label="AGS Item ID" value={playtest.agsItemId ?? '—'} />
          <FieldRow label="AGS Campaign ID" value={playtest.agsCampaignId ?? '—'} />
          <FieldRow label="Initial Quantity" value={playtest.initialCodeQuantity ?? '—'} />
          {!isLoading && stats && (
            <Space wrap>
              <Statistic title="Remaining" value={stats.unused ?? 0} />
              <Statistic title="Granted" value={stats.granted ?? 0} />
              <Statistic title="Total" value={stats.total ?? 0} />
            </Space>
          )}
          <Button onClick={() => navigate(`/${playtest.id ?? ''}/codes`)}>Sync / top up codes</Button>
        </Space>
      </Card>
    </Space>
  )
}

function FieldRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div style={{ display: 'flex', gap: 16, alignItems: 'baseline' }}>
      <Typography.Text strong style={{ width: 160 }}>
        {label}
      </Typography.Text>
      <Typography.Text>{value}</Typography.Text>
    </div>
  )
}
