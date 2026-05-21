import { CopyOutlined } from '@ant-design/icons'
import { useAppUIContext } from '@accelbyte/sdk-extend-app-ui'
import { useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Breadcrumb,
  Button,
  Card,
  Input,
  Modal,
  Space,
  Spin,
  Tabs,
  Tag,
  Typography,
  message
} from 'antd'
import dayjs from 'dayjs'
import { useNavigate, useParams, useSearchParams } from 'react-router'

import type { V1Playtest } from './playtesthubapi/generated-definitions/V1Playtest'
import {
  Key_PlaytesthubServiceAdmin,
  usePlaytesthubServiceAdminApi_CreatePlaytest_ByPlaytestIdTransitionStatuMutation,
  usePlaytesthubServiceAdminApi_GetPlaytests
} from './playtesthubapi/generated-admin/queries/PlaytesthubServiceAdmin.query'
import { usePlaytesthubServiceApi_GetConfig } from './playtesthubapi/generated-public/queries/PlaytesthubService.query'
import { DistributionModel, PlaytestStatus } from './shared/playtesthub-enums'
import { toastError } from './shared/api-error'
import { AuditTab } from './tabs/AuditTab'
import { DiscordBotToolsTab } from './tabs/DiscordBotToolsTab'
import { DistributionTab } from './tabs/DistributionTab'
import { ParticipantsTab } from './tabs/ParticipantsTab'
import { ResponsesTab } from './tabs/ResponsesTab'
import { SurveyTab } from './tabs/SurveyTab'

const STATUS_PILL: Record<string, { text: string; color: string }> = {
  [PlaytestStatus.DRAFT]: { text: 'Draft', color: 'default' },
  [PlaytestStatus.OPEN]: { text: 'Published', color: 'green' },
  [PlaytestStatus.CLOSED]: { text: 'Closed', color: 'red' }
}

const TABS = ['info', 'distribution', 'participants', 'bot-tools', 'survey', 'responses', 'audit'] as const
type TabKey = (typeof TABS)[number]

function isTabKey(v: string): v is TabKey {
  return (TABS as readonly string[]).includes(v)
}

export function PlaytestDetailPage() {
  const { slug } = useParams<{ slug: string }>()
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()

  const tabParam = searchParams.get('tab') ?? 'info'
  const activeTab: TabKey = isTabKey(tabParam) ? tabParam : 'info'

  const { data, isLoading, error, refetch } = usePlaytesthubServiceAdminApi_GetPlaytests(sdk, {})
  const playtest = ((data?.playtests ?? []) as V1Playtest[]).find(p => p.slug === slug)

  // Player-app origin is owned by the backend (PLAYER_BASE_URL env). Read it
  // via the unauth GetPublicConfig RPC so the admin AppUI never has to guess
  // — window.location.origin would point at the AGS Admin Portal host.
  const publicConfigQuery = usePlaytesthubServiceApi_GetConfig(sdk, {})
  const playerBaseUrl = publicConfigQuery.data?.playerBaseUrl ?? ''

  const transitionMutation = usePlaytesthubServiceAdminApi_CreatePlaytest_ByPlaytestIdTransitionStatuMutation(sdk, {
    onSuccess: () => {
      message.success('Status updated')
      queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Playtests] })
    },
    onError: toastError('update status')
  })

  if (isLoading) return <Spin size="large" data-testid="playtest-detail-loading" />
  if (error || !data) {
    return (
      <Alert
        type="error"
        message="Failed to load playtest"
        action={
          <Button size="small" onClick={() => refetch()}>
            Retry
          </Button>
        }
      />
    )
  }
  if (!playtest) {
    return (
      <Alert
        type="warning"
        message={`Playtest "${slug}" not found`}
        action={
          <Button size="small" onClick={() => navigate('/')}>
            Back to playtests
          </Button>
        }
      />
    )
  }

  const status = playtest.status ?? ''
  const pill = STATUS_PILL[status] ?? { text: status || '—', color: 'default' }
  const isDraft = status === PlaytestStatus.DRAFT
  const isOpen = status === PlaytestStatus.OPEN

  const handleTabChange = (key: string) => {
    const next = new URLSearchParams(searchParams)
    next.set('tab', key)
    setSearchParams(next, { replace: true })
  }

  const publish = () => {
    Modal.confirm({
      title: 'Publish this playtest?',
      content: 'Players can sign up once published.',
      okText: 'Publish',
      onOk: () =>
        new Promise<void>((resolve, reject) => {
          transitionMutation.mutate(
            { playtestId: playtest.id ?? '', data: { targetStatus: PlaytestStatus.OPEN } },
            { onSuccess: () => resolve(), onError: () => reject() }
          )
        })
    })
  }

  const stop = () => {
    Modal.confirm({
      title: 'Stop this playtest?',
      content:
        'Stopping this playtest will close it for new sign-ups. Approved players keep access to their codes / builds.',
      okText: 'Stop Playtest',
      okButtonProps: { danger: true },
      onOk: () =>
        new Promise<void>((resolve, reject) => {
          transitionMutation.mutate(
            { playtestId: playtest.id ?? '', data: { targetStatus: PlaytestStatus.CLOSED } },
            { onSuccess: () => resolve(), onError: () => reject() }
          )
        })
    })
  }

  const copyShareLink = () => {
    if (!playerBaseUrl) {
      message.error('Player app URL not configured — set PLAYER_BASE_URL on the backend')
      return
    }
    const link = `${playerBaseUrl.replace(/\/$/, '')}/#/playtest/${playtest.slug ?? ''}`
    void navigator.clipboard?.writeText(link).then(
      () => message.success('Playtest link copied'),
      () => message.error('Clipboard unavailable')
    )
  }

  return (
    <Space direction="vertical" style={{ width: '100%' }} data-testid="playtest-detail-page">
      <Breadcrumb
        items={[
          { title: 'Extend App UI' },
          { title: <a onClick={() => navigate('/')}>Playtest Hub</a> },
          { title: playtest.title ?? playtest.slug ?? '—' }
        ]}
      />
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 16 }}>
        <div>
          <Typography.Title level={2} style={{ margin: 0 }}>
            {playtest.title ?? '—'}
          </Typography.Title>
          <Space size={12} style={{ marginTop: 4 }}>
            <Typography.Text type="secondary">{formatDateRange(playtest.startsAt, playtest.endsAt)}</Typography.Text>
            <Button
              type="link"
              size="small"
              icon={<CopyOutlined />}
              iconPosition="end"
              onClick={copyShareLink}
              style={{ padding: 0, height: 'auto' }}
            >
              Playtest Link
            </Button>
          </Space>
        </div>
        <Space wrap data-testid="playtest-header-actions">
          <Tag color={pill.color} data-testid="playtest-status-pill" style={{ marginInlineEnd: 0 }}>
            {pill.text}
          </Tag>
          {isDraft && (
            <Button type="primary" onClick={publish} data-testid="header-publish">
              Publish
            </Button>
          )}
          {isOpen && (
            <Button onClick={stop} data-testid="header-stop">
              Stop Playtest
            </Button>
          )}
        </Space>
      </div>

      <Tabs
        activeKey={activeTab}
        onChange={handleTabChange}
        items={[
          {
            key: 'info',
            label: 'Playtest Info',
            children: <PlaytestInfoTab playtest={playtest} playerBaseUrl={playerBaseUrl} />
          },
          {
            key: 'distribution',
            label: 'Distribution',
            children: <DistributionTab playtest={playtest} />
          },
          { key: 'participants', label: 'Participants', children: <ParticipantsTab playtest={playtest} /> },
          { key: 'bot-tools', label: 'Discord Bot Tools', children: <DiscordBotToolsTab playtest={playtest} /> },
          { key: 'survey', label: 'Survey', children: <SurveyTab playtest={playtest} /> },
          { key: 'responses', label: 'Responses', children: <ResponsesTab playtest={playtest} /> },
          { key: 'audit', label: 'Audit', children: <AuditTab playtest={playtest} /> }
        ]}
      />
    </Space>
  )
}

const DISTRIBUTION_MODEL_LABEL: Record<string, string> = {
  [DistributionModel.ADT]: 'Direct Download (ADT)',
  [DistributionModel.STEAM_KEYS]: 'Steam Keys',
  [DistributionModel.AGS_CAMPAIGN]: 'AGS Campaign'
}

function PlaytestInfoTab({ playtest, playerBaseUrl }: { playtest: V1Playtest; playerBaseUrl: string }) {
  const navigate = useNavigate()

  const distributionLabel = playtest.distributionModel
    ? (DISTRIBUTION_MODEL_LABEL[playtest.distributionModel] ?? playtest.distributionModel)
    : '—'

  const rows: Array<[string, React.ReactNode]> = [
    ['Title', playtest.title ?? '—'],
    ['Slug', <Typography.Text code>{playtest.slug ?? '—'}</Typography.Text>],
    ['Description', playtest.description ?? '—'],
    ['Start Date', playtest.startsAt ? dayjs(playtest.startsAt).format('MMMM D, YYYY') : '—'],
    ['End Date', playtest.endsAt ? dayjs(playtest.endsAt).format('MMMM D, YYYY') : '—'],
    ['Platforms', (playtest.platforms ?? []).join(', ') || '—'],
    ['NDA Required', playtest.ndaRequired ? 'Yes' : 'No'],
    ['Distribution Model', distributionLabel],
    ['Approval Method', playtest.autoApprove ? 'Auto-Approve' : 'Manual'],
    ['Max Participants', playtest.autoApproveLimit ?? '—']
  ]

  const shareLink = playerBaseUrl ? `${playerBaseUrl.replace(/\/$/, '')}/#/playtest/${playtest.slug ?? ''}` : ''

  const copyShareLink = () => {
    if (!shareLink) {
      message.error('Player app URL not configured — set PLAYER_BASE_URL on the backend')
      return
    }
    void navigator.clipboard?.writeText(shareLink).then(
      () => message.success('Playtest link copied'),
      () => message.error('Clipboard unavailable')
    )
  }

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }} data-testid="playtest-info-tab">
      <Card
        title="Playtest Information"
        extra={
          <Button onClick={() => navigate(`/${playtest.id ?? ''}/edit`)} data-testid="playtest-info-edit">
            Edit
          </Button>
        }
        styles={{ body: { padding: 0 } }}
      >
        {rows.map(([label, value], idx) => (
          <div
            key={label}
            style={{
              display: 'flex',
              padding: '14px 24px',
              borderTop: idx === 0 ? 'none' : '1px solid #f0f0f0',
              alignItems: 'flex-start',
              fontSize: 14
            }}
          >
            <div style={{ width: 200, color: 'rgba(0, 0, 0, 0.65)', flexShrink: 0 }}>{label}</div>
            <div style={{ flex: 1, color: 'rgba(0, 0, 0, 0.88)' }}>{value}</div>
          </div>
        ))}
      </Card>

      <div data-testid="playtest-share-link">
        <Typography.Text type="secondary" style={{ display: 'block', marginBottom: 8 }}>
          Shareable Sign-Up Link
        </Typography.Text>
        <Input
          readOnly
          value={shareLink}
          style={{ fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, monospace' }}
          suffix={
            <Button type="link" size="small" onClick={copyShareLink} style={{ padding: 0 }}>
              Copy
            </Button>
          }
        />
      </div>
    </Space>
  )
}

function formatDateRange(starts?: string | null, ends?: string | null): string {
  if (!starts && !ends) return 'No dates set'
  if (!starts || !ends) {
    const only = starts ? dayjs(starts) : dayjs(ends!)
    return only.format('MMM D, YYYY')
  }
  const s = dayjs(starts)
  const e = dayjs(ends)
  if (s.year() === e.year()) {
    return `${s.format('MMM D')} – ${e.format('MMM D, YYYY')}`
  }
  return `${s.format('MMM D, YYYY')} – ${e.format('MMM D, YYYY')}`
}
