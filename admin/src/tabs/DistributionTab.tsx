import { CheckCircleFilled, CodeSandboxOutlined } from '@ant-design/icons'
import { useAppUIContext } from '@accelbyte/sdk-extend-app-ui'
import { useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Card,
  InputNumber,
  Space,
  Spin,
  Statistic,
  Table,
  Tag,
  Typography,
  Upload,
  message
} from 'antd'
import dayjs from 'dayjs'
import { useState } from 'react'
import type { V1AdtLinkage } from '../playtesthubapi/generated-definitions/V1AdtLinkage'
import type { V1Code } from '../playtesthubapi/generated-definitions/V1Code'
import type { V1CodePoolStats } from '../playtesthubapi/generated-definitions/V1CodePoolStats'
import type { V1Playtest } from '../playtesthubapi/generated-definitions/V1Playtest'
import type { V1UploadCodesRejection } from '../playtesthubapi/generated-definitions/V1UploadCodesRejection'
import {
  Key_PlaytesthubServiceAdmin,
  usePlaytesthubServiceAdminApi_CreateAdtBuildChange_ByPlaytestIdMutation,
  usePlaytesthubServiceAdminApi_CreateCodesSyncFromAg_ByPlaytestIdMutation,
  usePlaytesthubServiceAdminApi_CreateCodesTopUp_ByPlaytestIdMutation,
  usePlaytesthubServiceAdminApi_CreateCodesUpload_ByPlaytestIdMutation,
  usePlaytesthubServiceAdminApi_GetAdtLinkages,
  usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId,
  usePlaytesthubServiceAdminApi_GetGamesAdt_ByAdtLinkageId
} from '../playtesthubapi/generated-admin/queries/PlaytesthubServiceAdmin.query'
import { toastError } from '../shared/api-error'
import { ADTBuildPickerModal } from '../shared/adt-build-picker'
import { DistributionModel } from '../shared/playtesthub-enums'
import type { V1AdtGame } from '../playtesthubapi/generated-definitions/V1AdtGame'

const POOL_LOW_RATIO = 0.1

const CODE_STATE_TAG: Record<string, { text: string; color: string }> = {
  CODE_STATE_UNUSED: { text: 'Unused', color: 'default' },
  CODE_STATE_RESERVED: { text: 'Reserved', color: 'gold' },
  CODE_STATE_GRANTED: { text: 'Granted', color: 'green' }
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
  const { sdk } = useAppUIContext()
  const queryClient = useQueryClient()
  const linkagesQuery = usePlaytesthubServiceAdminApi_GetAdtLinkages(sdk, {})
  const linkages = (linkagesQuery.data?.linkages ?? []) as V1AdtLinkage[]
  const linkage = linkages.find(l => l.adtNamespace === playtest.adtNamespace) ?? null
  const linked = Boolean(playtest.adtNamespace)

  const [pickerOpen, setPickerOpen] = useState(false)

  const gamesQuery = usePlaytesthubServiceAdminApi_GetGamesAdt_ByAdtLinkageId(
    sdk,
    { adtLinkageId: linkage?.id ?? '' },
    { enabled: !!linkage?.id, retry: false }
  )
  const games = (gamesQuery.data?.games ?? []) as V1AdtGame[]

  const changeBuildMutation = usePlaytesthubServiceAdminApi_CreateAdtBuildChange_ByPlaytestIdMutation(sdk, {
    onSuccess: () => {
      message.success('Build updated')
      queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Playtests] })
      setPickerOpen(false)
    },
    onError: toastError('change build')
  })

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }} data-testid="distribution-tab">
      <Card
        data-testid="adt-connection-card"
        title="ADT Connection"
        extra={
          linked ? (
            <Tag color="green" style={{ marginInlineEnd: 0 }}>
              ● Connected
            </Tag>
          ) : (
            <Tag style={{ marginInlineEnd: 0 }}>Not Connected</Tag>
          )
        }>
        {linked ? (
          <div style={{ display: 'flex', gap: 48, alignItems: 'flex-start', flexWrap: 'wrap' }}>
            <FieldColumn label="ADT NAMESPACE" value={<code>{playtest.adtNamespace ?? '—'}</code>} />
            <FieldColumn label="LINKED AS" value={linkage?.linkedByUserId ?? '—'} />
            <FieldColumn
              label="LINKED ON"
              value={linkage?.linkedAt ? dayjs(linkage.linkedAt).format('MMM D, YYYY, h:mm A') : '—'}
            />
          </div>
        ) : (
          <Space direction="vertical">
            <Typography.Text strong>ADT Namespace Not Linked</Typography.Text>
            <Typography.Text type="secondary">
              Link your studio's ADT namespace to surface builds and approve players against this playtest.
            </Typography.Text>
            <Typography.Text type="secondary">
              Linking happens from the Playtests list page → Link new ADT Namespace.
            </Typography.Text>
          </Space>
        )}
      </Card>

      {linked && (
        <Card
          data-testid="adt-build-card"
          title="Game Build"
          extra={
            <Button onClick={() => setPickerOpen(true)} disabled={!linkage?.id}>
              Change Build
            </Button>
          }>
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            <div style={{ display: 'flex', gap: 16, alignItems: 'flex-start' }}>
              <div
                style={{
                  width: 56,
                  height: 56,
                  borderRadius: 8,
                  background: '#722ed1',
                  color: '#fff',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  fontSize: 28,
                  flexShrink: 0
                }}>
                <CodeSandboxOutlined />
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                <Typography.Text strong>{playtest.adtBuildId ?? '—'}</Typography.Text>
                <Typography.Text type="secondary" style={{ fontSize: 13 }}>
                  Game ID: <strong style={{ color: 'rgba(0,0,0,0.88)' }}>{playtest.adtGameId ?? '—'}</strong>
                  {playtest.adtFallbackDownloadUrl && (
                    <>
                      {'  ·  '}Fallback URL:{' '}
                      <a href={playtest.adtFallbackDownloadUrl} target="_blank" rel="noreferrer">
                        configured
                      </a>
                    </>
                  )}
                </Typography.Text>
              </div>
            </div>
            <Alert
              type="success"
              showIcon
              icon={<CheckCircleFilled />}
              message="Approved participants will receive a direct download link via Discord DM from the PlaytestHub bot."
            />
            <Typography.Text type="secondary" style={{ fontSize: 13 }}>
              Changing the build applies to future approvals and DM retries only — already-approved participants keep the
              download already sent.
            </Typography.Text>
          </Space>
        </Card>
      )}

      <ADTBuildPickerModal
        open={pickerOpen}
        adtLinkageId={linkage?.id ?? ''}
        initialGameId={playtest.adtGameId ?? ''}
        games={games}
        onCancel={() => setPickerOpen(false)}
        onPick={(gameId, buildId) =>
          changeBuildMutation.mutate({ playtestId: playtest.id ?? '', data: { adtGameId: gameId, adtBuildId: buildId } })
        }
      />
    </Space>
  )
}

function SteamKeysPanel({ playtest }: { playtest: V1Playtest }) {
  const { sdk } = useAppUIContext()
  const queryClient = useQueryClient()
  const playtestId = playtest.id ?? ''

  const codesQuery = usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId(sdk, { playtestId }, { enabled: !!playtestId })
  const invalidateCodes = () => queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Codes_ByPlaytestId] })

  const [csvText, setCsvText] = useState('')
  const [csvFilename, setCsvFilename] = useState('')
  const [rejections, setRejections] = useState<V1UploadCodesRejection[]>([])

  const uploadMutation = usePlaytesthubServiceAdminApi_CreateCodesUpload_ByPlaytestIdMutation(sdk, {
    onSuccess: response => {
      const r = (response.rejections ?? []) as V1UploadCodesRejection[]
      setRejections(r)
      if (r.length === 0) {
        message.success(`Inserted ${response.inserted ?? 0} codes`)
        setCsvText('')
        setCsvFilename('')
      } else {
        message.warning(`Upload rejected: ${r.length} invalid line${r.length === 1 ? '' : 's'}`)
      }
      invalidateCodes()
    },
    onError: toastError('upload codes')
  })

  const handleFileChosen = (file: File) => {
    const reader = new FileReader()
    reader.onload = () => {
      setCsvText(typeof reader.result === 'string' ? reader.result : '')
      setCsvFilename(file.name ?? '')
      setRejections([])
    }
    reader.readAsText(file)
    return false
  }

  const stats = codesQuery.data?.stats
  const codes = (codesQuery.data?.codes ?? []) as V1Code[]
  const total = stats?.total ?? 0

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }} data-testid="distribution-tab">
      <Card data-testid="code-pool-card" title="Code Pool">
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <LowPoolBanner stats={stats} />
          <PoolStatsRow stats={stats} />
        </Space>
      </Card>

      <Card data-testid="code-upload-card" title="Upload Steam Keys">
        <Space direction="vertical" size="small" style={{ width: '100%' }}>
          <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
            One code per line. UTF-8, max 10 MB, max 50,000 lines, charset <code>[A-Za-z0-9._-]</code>, length 1–128. Any
            invalid line rejects the whole file.
          </Typography.Paragraph>
          <Upload accept=".csv,.txt,text/plain,text/csv" beforeUpload={handleFileChosen} maxCount={1} showUploadList={false}>
            <Button>Choose file</Button>
          </Upload>
          {csvFilename && (
            <Typography.Paragraph style={{ marginBottom: 0 }}>
              Selected: <code>{csvFilename}</code>
            </Typography.Paragraph>
          )}
          <Button
            type="primary"
            disabled={!csvText}
            loading={uploadMutation.isPending}
            onClick={() => uploadMutation.mutate({ playtestId, data: { csvContent: csvText, filename: csvFilename || undefined } })}>
            Upload
          </Button>
          {rejections.length > 0 && (
            <Alert
              type="error"
              message={`Upload rejected — ${rejections.length} invalid line${rejections.length === 1 ? '' : 's'}`}
              description={
                <ul style={{ margin: 0, paddingLeft: 20 }}>
                  {rejections.slice(0, 50).map((rej, i) => (
                    <li key={i}>
                      Line {rej.lineNumber}: {rej.reason}
                      {rej.value ? ` — ${rej.value}` : ''}
                    </li>
                  ))}
                  {rejections.length > 50 && <li>…and {rejections.length - 50} more.</li>}
                </ul>
              }
            />
          )}
          {total === 0 && !codesQuery.isLoading && (
            <Alert type="info" showIcon message="No codes uploaded yet" description="Upload a CSV above to start approving applicants." />
          )}
        </Space>
      </Card>

      <Card data-testid="codes-card" title="Codes">
        <CodesTable query={codesQuery} codes={codes} />
      </Card>
    </Space>
  )
}

function AGSCampaignPanel({ playtest }: { playtest: V1Playtest }) {
  const { sdk } = useAppUIContext()
  const queryClient = useQueryClient()
  const playtestId = playtest.id ?? ''

  const codesQuery = usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId(sdk, { playtestId }, { enabled: !!playtestId })
  const invalidateCodes = () => queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Codes_ByPlaytestId] })

  const [topUpQty, setTopUpQty] = useState<number | null>(100)

  const topUpMutation = usePlaytesthubServiceAdminApi_CreateCodesTopUp_ByPlaytestIdMutation(sdk, {
    onSuccess: response => {
      message.success(`Generated ${response.added ?? 0} new codes`)
      invalidateCodes()
    },
    onError: toastError('top up')
  })

  const syncMutation = usePlaytesthubServiceAdminApi_CreateCodesSyncFromAg_ByPlaytestIdMutation(sdk, {
    onSuccess: response => {
      message.success(`Synced ${response.added ?? 0} new codes from AGS`)
      invalidateCodes()
    },
    onError: toastError('sync from AGS')
  })

  const stats = codesQuery.data?.stats
  const codes = (codesQuery.data?.codes ?? []) as V1Code[]

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }} data-testid="distribution-tab">
      <Card data-testid="ags-config-card" title="AGS Campaign Configuration">
        <Space direction="vertical" size="small" style={{ width: '100%' }}>
          <FieldRow label="AGS Item ID" value={playtest.agsItemId ?? '—'} />
          <FieldRow label="AGS Campaign ID" value={playtest.agsCampaignId ?? '—'} />
          <FieldRow label="Initial Quantity" value={playtest.initialCodeQuantity ?? '—'} />
        </Space>
      </Card>

      <Card data-testid="code-pool-card" title="Code Pool">
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <LowPoolBanner stats={stats} />
          <PoolStatsRow stats={stats} />
        </Space>
      </Card>

      <Card data-testid="code-generate-card" title="Generate / Sync AGS Campaign Codes">
        <Space direction="vertical" size="small" style={{ width: '100%' }}>
          <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
            Top-up calls AGS to generate fresh codes. Sync re-fetches from AGS to recover from a previous failure (idempotent).
          </Typography.Paragraph>
          <Space>
            <InputNumber min={1} max={50000} value={topUpQty} onChange={v => setTopUpQty(typeof v === 'number' ? v : null)} />
            <Button
              type="primary"
              disabled={!topUpQty || topUpQty < 1}
              loading={topUpMutation.isPending}
              onClick={() => topUpQty && topUpMutation.mutate({ playtestId, data: { quantity: topUpQty } })}>
              Generate more codes
            </Button>
            <Button loading={syncMutation.isPending} onClick={() => syncMutation.mutate({ playtestId, data: {} })}>
              Sync from AGS
            </Button>
          </Space>
        </Space>
      </Card>

      <Card data-testid="codes-card" title="Codes">
        <CodesTable query={codesQuery} codes={codes} />
      </Card>
    </Space>
  )
}

function PoolStatsRow({ stats }: { stats: V1CodePoolStats | null | undefined }) {
  return (
    <Space wrap size="large">
      <Statistic title="Total" value={stats?.total ?? 0} />
      <Statistic title="Unused" value={stats?.unused ?? 0} />
      <Statistic title="Reserved" value={stats?.reserved ?? 0} />
      <Statistic title="Granted" value={stats?.granted ?? 0} />
    </Space>
  )
}

function LowPoolBanner({ stats }: { stats: V1CodePoolStats | null | undefined }) {
  const total = stats?.total ?? 0
  const unused = stats?.unused ?? 0
  if (total <= 0) return null
  if (unused / total > POOL_LOW_RATIO) return null
  return (
    <Alert
      type="warning"
      showIcon
      message="Code pool is low"
      description={`Only ${unused} of ${total} codes remain unused. Top up before approving more applicants.`}
    />
  )
}

function CodesTable({
  query,
  codes
}: {
  query: { isLoading: boolean; error: unknown; refetch: () => void }
  codes: V1Code[]
}) {
  const columns = [
    { title: 'Value', dataIndex: 'value', key: 'value', render: (v: string | null | undefined) => v ?? '—' },
    {
      title: 'State',
      dataIndex: 'state',
      key: 'state',
      render: (v: string | null | undefined) => {
        const info = CODE_STATE_TAG[v ?? ''] ?? { text: v ?? '—', color: 'default' }
        return <Tag color={info.color}>{info.text}</Tag>
      }
    },
    {
      title: 'Created',
      dataIndex: 'createdAt',
      key: 'createdAt',
      render: (v: string | null | undefined) => (v ? dayjs(v).format('YYYY-MM-DD HH:mm') : '—')
    }
  ]
  if (query.isLoading) return <Spin />
  if (query.error) {
    return (
      <Alert
        type="error"
        message="Failed to load codes."
        action={
          <Button size="small" onClick={() => query.refetch()}>
            Retry
          </Button>
        }
      />
    )
  }
  return <Table<V1Code> rowKey={row => row.id ?? ''} dataSource={codes} columns={columns} pagination={{ pageSize: 50 }} />
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

function FieldColumn({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <Typography.Text type="secondary" style={{ fontSize: 11, letterSpacing: 0.5, textTransform: 'uppercase' }}>
        {label}
      </Typography.Text>
      <Typography.Text style={{ fontSize: 14 }}>{value}</Typography.Text>
    </div>
  )
}
