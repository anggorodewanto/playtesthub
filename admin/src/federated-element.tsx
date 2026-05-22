import { useAppUIContext } from '@accelbyte/sdk-extend-app-ui'
import { useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Card,
  Checkbox,
  DatePicker,
  Dropdown,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Radio,
  Select,
  Space,
  Spin,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
  message,
  type MenuProps
} from 'antd'
import dayjs, { type Dayjs } from 'dayjs'
import {
  DATE_RANGE_HELP,
  DATE_RANGE_LABEL,
  autoTransitionPreview,
  dateRangeUtcFromEvent,
  dateRangeWindowRule
} from './window'
import { useEffect, useMemo, useState } from 'react'
import { Route, Routes, useLocation, useNavigate, useParams, useSearchParams } from 'react-router'
import { PlaytestDetailPage } from './PlaytestDetailPage'
import type { V1AdtBuild } from './playtesthubapi/generated-definitions/V1AdtBuild'
import type { V1AdtGame } from './playtesthubapi/generated-definitions/V1AdtGame'
import type { V1AdtLinkage } from './playtesthubapi/generated-definitions/V1AdtLinkage'
import type { V1Playtest } from './playtesthubapi/generated-definitions/V1Playtest'
import type { V1WorkerHealthEntry } from './playtesthubapi/generated-definitions/V1WorkerHealthEntry'
import {
  Key_PlaytesthubServiceAdmin,
  usePlaytesthubServiceAdminApi_CreateAdtLinkagesCompleteMutation,
  usePlaytesthubServiceAdminApi_CreateAdtLinkagesRecoverMutation,
  usePlaytesthubServiceAdminApi_CreateAdtLinkagesStartMutation,
  usePlaytesthubServiceAdminApi_CreatePlaytestMutation,
  usePlaytesthubServiceAdminApi_CreatePlaytest_ByPlaytestIdTransitionStatuMutation,
  usePlaytesthubServiceAdminApi_DeleteAdtLinkage_ByAdtLinkageIdMutation,
  usePlaytesthubServiceAdminApi_DeletePlaytest_ByPlaytestIdMutation,
  usePlaytesthubServiceAdminApi_GetAdtLinkages,
  usePlaytesthubServiceAdminApi_GetBuildsAdt_ByAdtLinkageId,
  usePlaytesthubServiceAdminApi_GetGamesAdt_ByAdtLinkageId,
  usePlaytesthubServiceAdminApi_GetPlaytest_ByPlaytestId,
  usePlaytesthubServiceAdminApi_GetPlaytests,
  usePlaytesthubServiceAdminApi_GetWorkersHealth,
  usePlaytesthubServiceAdminApi_PatchPlaytest_ByPlaytestIdMutation
} from './playtesthubapi/generated-admin/queries/PlaytesthubServiceAdmin.query'

const PLATFORMS = [
  { value: 'PLATFORM_STEAM', label: 'Steam' },
  { value: 'PLATFORM_XBOX', label: 'Xbox' },
  { value: 'PLATFORM_PLAYSTATION', label: 'PlayStation' },
  { value: 'PLATFORM_EPIC', label: 'Epic' },
  { value: 'PLATFORM_OTHER', label: 'Other' }
] as const

import { DistributionModel, PlaytestStatus } from './shared/playtesthub-enums'
import { usePlaytesthubServiceApi_GetConfig } from './playtesthubapi/generated-public/queries/PlaytesthubService.query'
import { toastError } from './shared/api-error'

const STATUS_TAG: Record<string, { text: string; color: string }> = {
  [PlaytestStatus.DRAFT]: { text: 'Draft', color: 'default' },
  [PlaytestStatus.OPEN]: { text: 'Published', color: 'green' },
  [PlaytestStatus.CLOSED]: { text: 'Closed', color: 'red' }
}

function StatusTag({ status, startsAt, endsAt }: { status: string | null | undefined; startsAt?: string | null; endsAt?: string | null }) {
  const info = STATUS_TAG[status ?? ''] ?? { text: status ?? '—', color: 'default' }
  const preview = autoTransitionPreview(status, startsAt, endsAt)
  const tag = <Tag color={info.color}>{info.text}</Tag>
  if (!preview) return tag
  return <Tooltip title={preview}>{tag}</Tooltip>
}

const DISTRIBUTION_LABEL: Record<string, string> = {
  [DistributionModel.STEAM_KEYS]: 'Steam Keys',
  [DistributionModel.AGS_CAMPAIGN]: 'AGS Campaign Codes',
  [DistributionModel.ADT]: 'Direct Download (ADT)'
}

function WorkerHealthBanner() {
  const { sdk } = useAppUIContext()
  const { data } = usePlaytesthubServiceAdminApi_GetWorkersHealth(
    sdk,
    {},
    { refetchInterval: 30_000 }
  )
  const workers = (data?.workers ?? []) as V1WorkerHealthEntry[]
  const stale = workers.filter(w => w.stale)
  if (stale.length === 0) return null
  const names = stale.map(w => w.name ?? '').filter(Boolean).join(', ')
  return (
    <Alert
      type="error"
      showIcon
      style={{ marginBottom: 12 }}
      message="Background worker stale"
      description={`${names} hasn't ticked recently. Auto-transitions are paused — flip status manually via the Publish/Close buttons until ops investigates.`}
      data-testid="worker-health-banner"
    />
  )
}

export function FederatedElement() {
  return (
    <div style={{ padding: 16, backgroundColor: '#f0f2f5', minHeight: '100%' }}>
      <WorkerHealthBanner />
      <Routes>
        <Route path="/" index element={<PlaytestsListPage />} />
        <Route path="new" element={<PlaytestCreatePage />} />
        <Route path=":playtestId/edit" element={<PlaytestEditPage />} />
        <Route path="playtest/:slug" element={<PlaytestDetailPage />} />
        <Route path="adt-link-callback" element={<ADTLinkCallbackPage />} />
      </Routes>
    </div>
  )
}

// ADTLinkCallbackPage handles the redirect-back from ADT's linking
// flow. ADT round-trips `state` + `result` + `adt_namespace` on the
// query string; this page calls CompleteADTLink(state, adt_namespace)
// and navigates back to the playtests list on success. Per PRD §4.8.2
// no `grantCode` is read or forwarded — none is issued.
//
// 2026-05-21 recovery affordance: when ADT replies `result=failed`,
// the most common reason is `already_linked` (ADT still carries an
// orphan flag from a previous link that didn't soft-delete locally).
// The page exposes a "Recover existing linkage" button that calls
// RecoverADTLinkage(adt_namespace) so the operator no longer dead-ends
// at the error toast — the recovery agent shipped the RPC + CLI but
// the UI affordance had to wait for codegen.
//
// 2026-05-22 bug fix: ADT does NOT echo adt_namespace on failure
// callbacks (live URL observed in prod carried only state + reason +
// result=failed). The previous gate `result==='failed' && adtNamespace`
// therefore never rendered the button. We now always expose a recovery
// affordance on failure: when ADT echoed the namespace we one-click
// recover; otherwise we open an input where the operator types the
// namespace they intended to link.
function ADTLinkCallbackPage() {
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const [error, setError] = useState<string | null>(null)
  // canRetry is true only when the error came from the CompleteADTLink
  // mutation itself (network blip / gateway 5xx / backend Internal).
  // In that case the URL's state nonce may still be live (the backend
  // never reached ConsumePending), so refiring the mutation can
  // succeed. ADT-reported failures and missing-param errors have no
  // retry path — the operator must restart the link flow.
  const [canRetry, setCanRetry] = useState(false)
  const [recoverError, setRecoverError] = useState<string | null>(null)
  const [recovered, setRecovered] = useState(false)
  const [recoverPromptOpen, setRecoverPromptOpen] = useState(false)
  const [recoverNsInput, setRecoverNsInput] = useState('')

  const state = params.get('state') ?? ''
  const result = params.get('result') ?? ''
  const adtNamespace = params.get('adt_namespace') ?? ''
  const reason = params.get('reason') ?? ''

  const completeMutation = usePlaytesthubServiceAdminApi_CreateAdtLinkagesCompleteMutation(sdk, {
    onSuccess: () => {
      message.success('ADT namespace linked')
      navigate('/')
    },
    onError: (err: { message?: string }) => {
      setError(err.message ?? 'ADT linking failed')
      setCanRetry(true)
    }
  })

  const recoverMutation = usePlaytesthubServiceAdminApi_CreateAdtLinkagesRecoverMutation(sdk, {
    onSuccess: () => {
      setRecovered(true)
      message.success('ADT linkage recovered')
      navigate('/')
    },
    onError: (err: { message?: string }) => {
      setRecoverError(err.message ?? 'Recovering the ADT linkage failed')
    }
  })

  const runComplete = () => {
    setError(null)
    setCanRetry(false)
    completeMutation.mutate({ data: { state, adtNamespace } })
  }

  const runRecover = () => {
    setRecoverError(null)
    recoverMutation.mutate({ data: { adtNamespace } })
  }

  const runRecoverWithTypedNs = () => {
    const ns = recoverNsInput.trim()
    if (!ns) return
    setRecoverError(null)
    recoverMutation.mutate({ data: { adtNamespace: ns } })
  }

  useEffect(() => {
    if (result === 'failed') {
      setError(reason || 'ADT reported the link as failed')
      setCanRetry(false)
      return
    }
    if (!state || !adtNamespace) {
      setError('Callback is missing the state or adt_namespace query parameter')
      setCanRetry(false)
      return
    }
    completeMutation.mutate({ data: { state, adtNamespace } })
    // We deliberately depend only on the URL inputs — re-running on
    // mutation churn would refire the mutation.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state, adtNamespace, result, reason])

  if (error) {
    // Recovery is always meaningful on ADT-reported failure since the
    // orphan-flag scenario is the dominant cause. Two modes:
    //   - ADT echoed adt_namespace → one-click button (existing UX).
    //   - ADT omitted adt_namespace → reveal an inline input asking
    //     the operator to type the namespace they were linking.
    const isFailure = result === 'failed'
    const hasEchoedNs = Boolean(adtNamespace)
    const showOneClickRecover = isFailure && hasEchoedNs
    const showTypedRecover = isFailure && !hasEchoedNs
    const isAlreadyLinked = reason === 'already_linked'
    const submitDisabled = recoverNsInput.trim() === '' || recoverMutation.isPending || recovered
    return (
      <Space direction="vertical" style={{ width: '100%' }} data-testid="adt-link-callback">
        <Alert type="error" message="ADT linking failed" description={error} showIcon />
        {recoverError && (
          <Alert
            type="error"
            message="Recovery failed"
            description={recoverError}
            showIcon
            data-testid="adt-link-callback-recover-error"
          />
        )}
        <Space>
          {canRetry && (
            <Button
              type="primary"
              onClick={runComplete}
              loading={completeMutation.isPending}
              data-testid="adt-link-callback-retry"
            >
              Retry
            </Button>
          )}
          {showOneClickRecover && (
            <Button
              type={isAlreadyLinked ? 'primary' : 'default'}
              onClick={runRecover}
              loading={recoverMutation.isPending}
              disabled={recovered}
              data-testid="adt-link-callback-recover"
            >
              {isAlreadyLinked
                ? 'Recover existing linkage'
                : 'If you believe ADT already has this linkage, try Recover'}
            </Button>
          )}
          {showTypedRecover && !recoverPromptOpen && (
            <Button
              type="primary"
              onClick={() => setRecoverPromptOpen(true)}
              disabled={recovered}
              data-testid="adt-link-callback-recover-prompt"
            >
              Recover existing linkage
            </Button>
          )}
          <Button onClick={() => navigate('/')}>Back to playtests</Button>
        </Space>
        {showTypedRecover && recoverPromptOpen && (
          <Space direction="vertical" style={{ width: '100%' }}>
            <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
              ADT did not echo the namespace back. Enter the ADT namespace you were trying to link and we will adopt the
              orphan linkage on this side.
            </Typography.Paragraph>
            <Input
              placeholder="ADT namespace (e.g. studio-pong-dev)"
              value={recoverNsInput}
              onChange={e => setRecoverNsInput(e.target.value)}
              onPressEnter={() => {
                if (!submitDisabled) runRecoverWithTypedNs()
              }}
              data-testid="adt-link-callback-recover-input"
            />
            <Space>
              <Button
                type="primary"
                onClick={runRecoverWithTypedNs}
                loading={recoverMutation.isPending}
                disabled={submitDisabled}
                data-testid="adt-link-callback-recover-submit"
              >
                Recover
              </Button>
              <Button onClick={() => setRecoverPromptOpen(false)}>Cancel</Button>
            </Space>
          </Space>
        )}
      </Space>
    )
  }
  return (
    <div data-testid="adt-link-callback">
      <Spin size="large" />
      <Typography.Paragraph style={{ marginTop: 12 }}>Finalizing ADT link…</Typography.Paragraph>
    </div>
  )
}

function PlaytestsListPage() {
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const { data, isLoading, error, refetch } = usePlaytesthubServiceAdminApi_GetPlaytests(sdk, {})
  const publicConfigQuery = usePlaytesthubServiceApi_GetConfig(sdk, {})
  const playerBaseUrl = publicConfigQuery.data?.playerBaseUrl ?? ''

  const deleteMutation = usePlaytesthubServiceAdminApi_DeletePlaytest_ByPlaytestIdMutation(sdk, {
    onSuccess: () => {
      message.success('Playtest deleted')
      queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Playtests] })
    },
    onError: toastError('delete')
  })
  const transitionMutation = usePlaytesthubServiceAdminApi_CreatePlaytest_ByPlaytestIdTransitionStatuMutation(sdk, {
    onSuccess: () => {
      message.success('Status updated')
      queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Playtests] })
    },
    onError: toastError('update status')
  })

  const playtests = (data?.playtests ?? []) as V1Playtest[]

  const copyLink = (slug: string) => {
    if (!playerBaseUrl) {
      message.error('Player app URL not configured — set PLAYER_BASE_URL on the backend')
      return
    }
    const link = `${playerBaseUrl.replace(/\/$/, '')}/#/playtest/${slug}`
    void navigator.clipboard?.writeText(link).then(
      () => message.success('Playtest link copied'),
      () => message.error('Clipboard unavailable')
    )
  }

  const confirmPublish = (row: V1Playtest) =>
    Modal.confirm({
      title: 'Publish this playtest?',
      content: 'Players will be able to see it and sign up.',
      okText: 'Publish',
      onOk: () =>
        new Promise<void>((resolve, reject) => {
          transitionMutation.mutate(
            { playtestId: row.id ?? '', data: { targetStatus: PlaytestStatus.OPEN } },
            { onSuccess: () => resolve(), onError: () => reject() }
          )
        })
    })

  const confirmStop = (row: V1Playtest) =>
    Modal.confirm({
      title: 'Stop this playtest?',
      content: 'Players can no longer sign up. Existing applicants keep their state.',
      okText: 'Stop Playtest',
      okButtonProps: { danger: true },
      onOk: () =>
        new Promise<void>((resolve, reject) => {
          transitionMutation.mutate(
            { playtestId: row.id ?? '', data: { targetStatus: PlaytestStatus.CLOSED } },
            { onSuccess: () => resolve(), onError: () => reject() }
          )
        })
    })

  const confirmDelete = (row: V1Playtest) =>
    Modal.confirm({
      title: 'Soft-delete this playtest?',
      content: 'Row will be hidden from players. Applicants + codes are preserved.',
      okText: 'Delete',
      okButtonProps: { danger: true },
      onOk: () =>
        new Promise<void>((resolve, reject) => {
          deleteMutation.mutate(
            { playtestId: row.id ?? '' },
            { onSuccess: () => resolve(), onError: () => reject() }
          )
        })
    })

  const columns = [
    {
      title: 'Title',
      dataIndex: 'title',
      key: 'title',
      render: (value: string | null | undefined) => <Typography.Text strong>{value ?? '—'}</Typography.Text>
    },
    {
      title: 'Slug',
      dataIndex: 'slug',
      key: 'slug',
      render: (value: string | null | undefined) => (value ? <Typography.Text code>{value}</Typography.Text> : '—')
    },
    {
      title: 'Distribution',
      dataIndex: 'distributionModel',
      key: 'distributionModel',
      render: (value: string | null | undefined) => DISTRIBUTION_LABEL[value ?? ''] ?? value ?? '—'
    },
    {
      title: 'Approval',
      dataIndex: 'autoApprove',
      key: 'autoApprove',
      render: (value: boolean | null | undefined) =>
        value ? <Tag color="green">Auto-Approve</Tag> : <Tag color="orange">Manual</Tag>
    },
    {
      title: 'Status',
      dataIndex: 'status',
      key: 'status',
      render: (_: unknown, row: V1Playtest) => <StatusTag status={row.status} startsAt={row.startsAt} endsAt={row.endsAt} />
    },
    {
      title: 'Updated',
      dataIndex: 'updatedAt',
      key: 'updatedAt',
      render: (value: string | null | undefined) => (value ? dayjs(value).format('M/D/YYYY, h:mm A') : '—')
    },
    {
      title: 'Action',
      key: 'actions',
      render: (_: unknown, row: V1Playtest) => {
        const isDraft = row.status === PlaytestStatus.DRAFT
        const isOpen = row.status === PlaytestStatus.OPEN

        const menuItems: MenuProps['items'] = [
          { key: 'edit', label: 'Edit', onClick: () => navigate(`${row.id}/edit`, { state: { from: '/' } }) },
          { key: 'copy', label: 'Copy Link', onClick: () => copyLink(row.slug ?? '') },
          ...(isDraft
            ? [{ key: 'publish', label: 'Publish', onClick: () => confirmPublish(row) }]
            : []),
          ...(isOpen
            ? [{ key: 'stop', label: 'Stop Playtest', danger: true, onClick: () => confirmStop(row) }]
            : []),
          { key: 'delete', label: 'Delete', danger: true, onClick: () => confirmDelete(row) }
        ]

        return (
          <Space size="small">
            <Button type="link" size="small" onClick={() => navigate(`playtest/${row.slug ?? ''}`)}>
              View
            </Button>
            <Dropdown menu={{ items: menuItems }} trigger={['click']} placement="bottomRight">
              <Button type="text" size="small" aria-label="More actions">
                ⋯
              </Button>
            </Dropdown>
          </Space>
        )
      }
    }
  ]

  return (
    <>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <Typography.Title level={2} style={{ margin: 0 }}>
          Playtest Hub
        </Typography.Title>
        <Button type="primary" onClick={() => navigate('new')}>
          + Create Playtest
        </Button>
      </div>

      {error && (
        <Alert
          type="error"
          message="Failed to load playtests."
          style={{ marginBottom: 16 }}
          action={
            <Button size="small" onClick={() => refetch()}>
              Retry
            </Button>
          }
        />
      )}
      <Card title="Playtest List">
        {isLoading ? (
          <Spin description="Loading playtests..." />
        ) : (
          <Table<V1Playtest>
            rowKey={row => row.id ?? row.slug ?? ''}
            dataSource={playtests}
            columns={columns}
            pagination={{ pageSize: 20 }}
          />
        )}
      </Card>
      <ADTLinkagesPanel />
    </>
  )
}

type FormValues = {
  slug?: string
  title: string
  description?: string
  bannerImageUrl?: string
  platforms: string[]
  dateRange?: [Dayjs, Dayjs]
  ndaRequired?: boolean
  ndaText?: string
  distributionModel: string
  initialCodeQuantity?: number
  autoApprove?: boolean
  autoApproveLimit?: number
  // M5.B ADT fields. adtLinkageId is the in-form selector; the create
  // mutation submits adtNamespace from the picked linkage row.
  adtLinkageId?: string
  adtNamespace?: string
  adtGameId?: string
  adtBuildId?: string
}

const AUTO_APPROVE_LIMIT_MIN = 1
const AUTO_APPROVE_LIMIT_MAX = 100000
const AUTO_APPROVE_LIMIT_ERROR =
  'auto_approve_limit must be between 1 and 100000 when auto_approve is true'

// Mirrors the errors.md row for CreatePlaytest / EditPlaytest banner URL —
// the backend rejects http with the byte-exact "banner_image_url must be
// an https URL" string; the client validator surfaces the same string so
// the form behaves the same whether the server is in the loop or not.
const bannerImageUrlRule = {
  validator(_: unknown, value: unknown) {
    if (value == null || value === '') return Promise.resolve()
    if (typeof value !== 'string') return Promise.reject(new Error('banner_image_url must be an https URL'))
    try {
      const parsed = new URL(value)
      if (parsed.protocol !== 'https:') {
        return Promise.reject(new Error('banner_image_url must be an https URL'))
      }
      return Promise.resolve()
    } catch {
      return Promise.reject(new Error('banner_image_url must be an https URL'))
    }
  }
}

const autoApproveLimitRule = ({ getFieldValue }: { getFieldValue: (name: string) => unknown }) => ({
  validator(_: unknown, value: unknown) {
    if (!getFieldValue('autoApprove')) return Promise.resolve()
    if (typeof value !== 'number' || !Number.isInteger(value) || value < AUTO_APPROVE_LIMIT_MIN || value > AUTO_APPROVE_LIMIT_MAX) {
      return Promise.reject(new Error(AUTO_APPROVE_LIMIT_ERROR))
    }
    return Promise.resolve()
  }
})

// ADTLinkagesPanel renders the studio-scoped ADT linkages list + a
// "Link new ADT Namespace" affordance. Lives at the bottom of the
// PlaytestsListPage per STATUS_M5.md B7 — linkage is studio-scoped, not
// playtest-scoped, so it stays out of the create-playtest form.
function ADTLinkagesPanel() {
  const { sdk } = useAppUIContext()
  const queryClient = useQueryClient()
  const { data, isLoading, error } = usePlaytesthubServiceAdminApi_GetAdtLinkages(sdk, {})
  const unlinkMutation = usePlaytesthubServiceAdminApi_DeleteAdtLinkage_ByAdtLinkageIdMutation(sdk, {
    onSuccess: () => {
      message.success('ADT linkage removed')
      queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.AdtLinkages] })
    },
    onError: toastError('unlink')
  })
  const [modalOpen, setModalOpen] = useState(false)
  const linkages = (data?.linkages ?? []) as V1AdtLinkage[]

  const columns = [
    { title: 'ADT namespace', dataIndex: 'adtNamespace', key: 'adtNamespace' },
    { title: 'Studio namespace', dataIndex: 'studioNamespace', key: 'studioNamespace' },
    { title: 'Linked at', dataIndex: 'linkedAt', key: 'linkedAt' },
    {
      title: 'Actions',
      key: 'actions',
      render: (_: unknown, row: V1AdtLinkage) => (
        <Popconfirm
          title="Unlink this ADT namespace?"
          description="Subsequent ADT API calls will fail until re-linked."
          onConfirm={() => unlinkMutation.mutate({ adtLinkageId: row.id ?? '' })}>
          <Button size="small" danger>
            Unlink
          </Button>
        </Popconfirm>
      )
    }
  ]
  return (
    <div style={{ marginTop: 24 }} data-testid="adt-linkages-panel">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
        <Typography.Title level={3} style={{ margin: 0 }}>
          ADT Linkages
        </Typography.Title>
        <Button onClick={() => setModalOpen(true)}>Link new ADT Namespace</Button>
      </div>
      <Typography.Paragraph type="secondary">
        Studio-wide linkage — one row covers every game namespace under your studio.
      </Typography.Paragraph>
      {isLoading && <Spin />}
      {error && <Alert type="error" message="Failed to load linkages" showIcon />}
      {!isLoading && !error && (
        <Table<V1AdtLinkage>
          rowKey={row => row.id ?? ''}
          dataSource={linkages}
          columns={columns}
          pagination={false}
          locale={{ emptyText: 'No ADT linkages yet. Click "Link new ADT Namespace" to link one.' }}
        />
      )}
      <LinkADTModal open={modalOpen} onClose={() => setModalOpen(false)} />
    </div>
  )
}

// LinkADTModal mirrors STATUS_M5.md B7's "Link new ADT Namespace"
// modal: copy + Cancel / Proceed. Proceed calls StartADTLink → assigns
// window.location.href to the returned linkUrl. Any in-progress create
// form draft has already been persisted by the form's onValuesChange
// hook (sessionStorage); operators land back on /adt-link-callback.
function LinkADTModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { sdk } = useAppUIContext()
  const startMutation = usePlaytesthubServiceAdminApi_CreateAdtLinkagesStartMutation(sdk, {
    onSuccess: data => {
      if (data?.linkUrl) {
        window.location.href = data.linkUrl
        return
      }
      message.error('ADT did not return a link URL')
    },
    onError: toastError('start ADT link')
  })
  return (
    <Modal
      open={open}
      title="Link ADT Namespace"
      okText="Proceed"
      onOk={() => startMutation.mutate({ data: {} })}
      confirmLoading={startMutation.isPending}
      onCancel={onClose}>
      <Typography.Paragraph>
        You will be redirected to ADT to authorise the linkage. After ADT confirms, you'll return here automatically.
      </Typography.Paragraph>
      <Typography.Paragraph type="secondary">
        No credential is exchanged — playtesthub authenticates to ADT via your studio's AGS service token on every API call.
      </Typography.Paragraph>
    </Modal>
  )
}

// ADTCreateFields renders the ADT linkage Select + Select Game Build picker affordance inside the Distribution Model section. adtGameId + adtBuildId are written via the picker modal (B13).
function ADTCreateFields({
  form,
  linkageId,
  adtGameId,
  adtBuildId
}: {
  form: ReturnType<typeof Form.useForm<FormValues>>[0]
  linkageId: string
  adtGameId: string
  adtBuildId: string
}) {
  const { sdk } = useAppUIContext()
  const [pickerOpen, setPickerOpen] = useState(false)
  const linkagesQuery = usePlaytesthubServiceAdminApi_GetAdtLinkages(sdk, {})
  const gamesQuery = usePlaytesthubServiceAdminApi_GetGamesAdt_ByAdtLinkageId(
    sdk,
    { adtLinkageId: linkageId },
    { enabled: !!linkageId, retry: false }
  )
  const linkages = (linkagesQuery.data?.linkages ?? []) as V1AdtLinkage[]
  const games = (gamesQuery.data?.games ?? []) as V1AdtGame[]

  const linkageSelect = (
    <Form.Item label="ADT linkage" name="adtLinkageId" rules={[{ required: true, message: 'Pick a linked ADT namespace' }]}>
      <Select
        placeholder="Select a linked ADT namespace"
        options={linkages.map(l => ({ value: l.id, label: `${l.adtNamespace ?? ''} (${l.studioNamespace ?? ''})` }))}
        onChange={(_id: string) => {
          const picked = linkages.find(l => l.id === _id)
          form.setFieldValue('adtNamespace', picked?.adtNamespace ?? '')
          // Linkage changed → invalidate any previously picked game/build so
          // we never submit cross-linkage IDs.
          form.setFieldValue('adtGameId', undefined)
          form.setFieldValue('adtBuildId', undefined)
        }}
      />
    </Form.Item>
  )

  const pickedGame = games.find(g => g.id === adtGameId)
  const pickedSummary =
    adtGameId && adtBuildId ? (
      <Typography.Text type="secondary">
        Picked: <Typography.Text strong>{pickedGame?.name ?? adtGameId}</Typography.Text> /{' '}
        <Typography.Text code>{adtBuildId}</Typography.Text>
      </Typography.Text>
    ) : (
      <Typography.Text type="secondary">No build picked yet.</Typography.Text>
    )
  return (
    <>
      {linkageSelect}
      <Form.Item name="adtNamespace" hidden rules={[{ required: true, message: 'Pick an ADT linkage' }]}>
        <Input />
      </Form.Item>
      <Form.Item name="adtGameId" hidden rules={[{ required: true, message: 'Pick a game build via the picker' }]}>
        <Input />
      </Form.Item>
      <Form.Item name="adtBuildId" hidden rules={[{ required: true, message: 'Pick a build via the picker' }]}>
        <Input />
      </Form.Item>
      <Form.Item label="Game build">
        <Space direction="vertical" style={{ width: '100%' }}>
          <Button onClick={() => setPickerOpen(true)} disabled={!linkageId} data-testid="adt-open-picker">
            Select Game Build
          </Button>
          {pickedSummary}
        </Space>
      </Form.Item>
      <ADTBuildPickerModal
        open={pickerOpen}
        adtLinkageId={linkageId}
        initialGameId={adtGameId || (games[0]?.id ?? '')}
        games={games}
        onCancel={() => setPickerOpen(false)}
        onPick={(gameId, buildId) => {
          form.setFieldValue('adtGameId', gameId)
          form.setFieldValue('adtBuildId', buildId)
          setPickerOpen(false)
        }}
      />
    </>
  )
}

// ADTBuildPickerModal renders the namespace → game → version → build
// picker that B13 specs against docs/images/build-picker-mockup.png.
// Game dropdown lives at the top; versions are derived by grouping
// ListADTBuilds on Build.name (= game_version_name); the right rail
// renders per-platform cards. "Use This Build" lifts (gameId, buildId)
// to the parent form.
function ADTBuildPickerModal({
  open,
  adtLinkageId,
  initialGameId,
  games,
  onCancel,
  onPick
}: {
  open: boolean
  adtLinkageId: string
  initialGameId: string
  games: V1AdtGame[]
  onCancel: () => void
  onPick: (gameId: string, buildId: string) => void
}) {
  const { sdk } = useAppUIContext()
  const [gameId, setGameId] = useState(initialGameId)
  const [versionName, setVersionName] = useState<string | null>(null)
  const [pickedBuildId, setPickedBuildId] = useState<string | null>(null)

  const buildsQuery = usePlaytesthubServiceAdminApi_GetBuildsAdt_ByAdtLinkageId(
    sdk,
    { adtLinkageId, queryParams: { adtGameId: gameId } },
    { enabled: open && !!adtLinkageId && !!gameId }
  )
  const buildsData = buildsQuery.data?.builds
  const versionGroups = useMemo(() => {
    const groups = new Map<string, V1AdtBuild[]>()
    for (const b of (buildsData ?? []) as V1AdtBuild[]) {
      const key = b.name ?? '—'
      const list = groups.get(key) ?? []
      list.push(b)
      groups.set(key, list)
    }
    return Array.from(groups.entries())
  }, [buildsData])

  const versionBuilds = versionName ? (versionGroups.find(([k]) => k === versionName)?.[1] ?? []) : []

  const handleUseBuild = () => {
    if (!gameId || !pickedBuildId) return
    onPick(gameId, pickedBuildId)
  }

  return (
    <Modal
      open={open}
      title="Select Game Build"
      width={780}
      destroyOnClose
      onCancel={onCancel}
      okText="Use This Build"
      okButtonProps={{ disabled: !gameId || !pickedBuildId }}
      onOk={handleUseBuild}>
      <Typography.Paragraph type="secondary">
        Choose a game, select a version, then pick a specific build to use for this playtest.
      </Typography.Paragraph>
      <div style={{ display: 'flex', gap: 16 }}>
        <div style={{ width: 280 }}>
          <Select
            style={{ width: '100%', marginBottom: 12 }}
            value={gameId || undefined}
            placeholder="Pick a game"
            onChange={(v: string) => {
              setGameId(v)
              setVersionName(null)
              setPickedBuildId(null)
            }}
            options={games.map(g => ({ value: g.id ?? '', label: g.name ?? g.id ?? '' }))}
          />
          <div
            style={{
              border: '1px solid #f0f0f0',
              borderRadius: 6,
              minHeight: 360,
              maxHeight: 360,
              overflowY: 'auto'
            }}>
            <Typography.Text type="secondary" style={{ display: 'block', padding: '8px 12px', textTransform: 'uppercase', fontSize: 12 }}>
              Versions
            </Typography.Text>
            {versionGroups.length === 0 && (
              <Typography.Paragraph type="secondary" style={{ padding: '8px 12px' }}>
                {buildsQuery.isLoading ? 'Loading…' : 'No builds for this game.'}
              </Typography.Paragraph>
            )}
            {versionGroups.map(([name, group]) => {
              const selected = name === versionName
              const count = group.length
              const countLabel = count === 1 ? '1 build' : `${count} builds`
              return (
                <button
                  key={name}
                  type="button"
                  onClick={() => {
                    setVersionName(name)
                    setPickedBuildId(null)
                  }}
                  data-testid={`adt-picker-version-${name}`}
                  style={{
                    display: 'block',
                    width: '100%',
                    textAlign: 'left',
                    padding: '10px 12px',
                    background: selected ? '#e6f4ff' : 'transparent',
                    border: 'none',
                    borderTop: '1px solid #f0f0f0',
                    cursor: 'pointer'
                  }}>
                  <Typography.Text strong>{name}</Typography.Text>
                  <div>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {countLabel}
                    </Typography.Text>
                  </div>
                </button>
              )
            })}
          </div>
        </div>
        <div style={{ flex: 1, minHeight: 360, border: '1px solid #f0f0f0', borderRadius: 6, padding: 16 }}>
          {!versionName && (
            <div style={{ textAlign: 'center', paddingTop: 80 }}>
              <Typography.Title level={5} style={{ marginBottom: 4 }}>
                Select a version
              </Typography.Title>
              <Typography.Paragraph type="secondary">Choose a version from the left to see available builds.</Typography.Paragraph>
              <Typography.Text type="secondary">No version selected</Typography.Text>
            </div>
          )}
          {versionName && (
            <Space direction="vertical" style={{ width: '100%' }} size={12}>
              {versionBuilds.map(b => {
                const selected = b.id === pickedBuildId
                return (
                  <Card
                    key={b.id ?? ''}
                    size="small"
                    hoverable
                    onClick={() => setPickedBuildId(b.id ?? null)}
                    style={{ borderColor: selected ? '#1677ff' : undefined }}
                    data-testid={`adt-picker-build-${b.id}`}>
                    <Space direction="vertical" size={2}>
                      <Typography.Text strong>{b.platform ?? 'unknown platform'}</Typography.Text>
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        Uploaded {b.uploadedAt ?? '—'}
                      </Typography.Text>
                      <Typography.Text code>{b.id ?? ''}</Typography.Text>
                    </Space>
                  </Card>
                )
              })}
            </Space>
          )}
        </div>
      </div>
    </Modal>
  )
}

// SectionRow renders the two-column section layout used by PlaytestCreatePage:
// title + secondary description on the left rail, form fields on the right.
function SectionRow({
  title,
  description,
  children
}: {
  title: string
  description?: string
  children: React.ReactNode
}) {
  return (
    <div style={{ display: 'flex', gap: 48, padding: '24px 0' }}>
      <div style={{ width: 240, flexShrink: 0 }}>
        <Typography.Title level={5} style={{ margin: 0 }}>
          {title}
        </Typography.Title>
        {description && (
          <Typography.Paragraph type="secondary" style={{ marginTop: 6, marginBottom: 0, fontSize: 13 }}>
            {description}
          </Typography.Paragraph>
        )}
      </div>
      <div style={{ flex: 1, minWidth: 0 }}>{children}</div>
    </div>
  )
}

// PlatformsPills implements the value/onChange contract Form.Item injects so it
// renders as a toggleable button row inside the form's data flow.
function PlatformsPills({
  value,
  onChange,
  options
}: {
  value?: string[]
  onChange?: (v: string[]) => void
  options: ReadonlyArray<{ value: string; label: string }>
}) {
  const selected = new Set(value ?? [])
  const toggle = (v: string) => {
    const next = new Set(selected)
    if (next.has(v)) next.delete(v)
    else next.add(v)
    onChange?.(Array.from(next))
  }
  return (
    <div data-testid="platforms-select" role="group" aria-label="Platforms" style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
      {options.map(o => {
        const active = selected.has(o.value)
        return (
          <Button
            key={o.value}
            type={active ? 'primary' : 'default'}
            ghost={active}
            onClick={() => toggle(o.value)}
            aria-pressed={active}>
            {o.label}
          </Button>
        )
      })}
    </div>
  )
}

const radioCardStyle = (active: boolean): React.CSSProperties => ({
  display: 'flex',
  alignItems: 'flex-start',
  width: '100%',
  margin: 0,
  padding: 16,
  border: '1px solid',
  borderColor: active ? '#1677ff' : '#e6e8eb',
  borderRadius: 8,
  background: active ? '#f0f7ff' : '#fff'
})

function RadioCardLabel({
  title,
  description,
  badge
}: {
  title: React.ReactNode
  description: React.ReactNode
  badge?: React.ReactNode
}) {
  return (
    <div style={{ paddingLeft: 4 }}>
      <Space size={8} align="center" wrap>
        <Typography.Text strong>{title}</Typography.Text>
        {badge}
      </Space>
      <Typography.Paragraph type="secondary" style={{ marginTop: 4, marginBottom: 0, fontSize: 13 }}>
        {description}
      </Typography.Paragraph>
    </div>
  )
}

// DistributionRadioCards renders the three distribution-model options as full-
// width radio cards. The ADT row carries an inline "linking required" warning
// when no ADT linkage is configured so operators see the gating reason without
// drilling into the picker. value/onChange come from the wrapping Form.Item.
function DistributionRadioCards({
  value,
  onChange,
  linkageCount
}: {
  value?: string
  onChange?: (v: string) => void
  linkageCount: number
}) {
  const adtBadge =
    linkageCount === 0 ? (
      <Tag color="warning" style={{ marginInlineStart: 0 }}>
        ⚠ ADT namespace linking required
      </Tag>
    ) : null
  // aria-label pins each radio's accessible name to the bare title so tests
  // (and screen readers) can disambiguate without dragging the description
  // paragraph into the name.
  return (
    <Radio.Group
      value={value}
      onChange={e => onChange?.(e.target.value)}
      style={{ display: 'flex', flexDirection: 'column', gap: 8, width: '100%' }}>
      <Radio value={DistributionModel.STEAM_KEYS} aria-label="Steam keys" style={radioCardStyle(value === DistributionModel.STEAM_KEYS)}>
        <RadioCardLabel
          title="Steam keys"
          description="Upload a CSV of Steam keys. Approved players receive a key via Discord DM and redeem it manually on Steam."
        />
      </Radio>
      <Radio value={DistributionModel.AGS_CAMPAIGN} aria-label="AGS Campaign" style={radioCardStyle(value === DistributionModel.AGS_CAMPAIGN)}>
        <RadioCardLabel
          title="AGS Campaign"
          description="Auto-generate redeemable codes via AGS Platform Campaign API. Players redeem codes in-game through the AGS entitlement system."
        />
      </Radio>
      <Radio value={DistributionModel.ADT} aria-label="ADT" style={radioCardStyle(value === DistributionModel.ADT)}>
        <RadioCardLabel
          title="ADT"
          description="Distribute game builds directly via AccelByte Development Toolkit (ADT). Approved players receive a direct download link via Discord DM — no additional launcher required. Supports crash reporting and hardware telemetry."
          badge={adtBadge}
        />
      </Radio>
    </Radio.Group>
  )
}

function ApprovalRadioCards({
  value,
  onChange
}: {
  value?: boolean
  onChange?: (v: boolean) => void
}) {
  return (
    <Radio.Group
      value={value}
      onChange={e => onChange?.(e.target.value)}
      style={{ display: 'flex', flexDirection: 'column', gap: 8, width: '100%' }}>
      <Radio value={false} aria-label="Manual Approval" style={radioCardStyle(value === false)}>
        <RadioCardLabel
          title="Manual Approval"
          description="Review each sign-up and manually approve or reject participants from the admin panel."
        />
      </Radio>
      <Radio value={true} aria-label="Auto-Approve" style={radioCardStyle(value === true)}>
        <RadioCardLabel
          title="Auto-Approve"
          description="Automatically approve participants on sign-up, up to a maximum capacity. No manual review required."
        />
      </Radio>
    </Radio.Group>
  )
}

function PlaytestCreatePage() {
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [form] = Form.useForm<FormValues>()
  const distributionModel = Form.useWatch('distributionModel', form)
  const autoApprove = Form.useWatch('autoApprove', form)
  const ndaRequired = Form.useWatch('ndaRequired', form)
  const adtLinkageId = Form.useWatch('adtLinkageId', form)
  const adtGameId = Form.useWatch('adtGameId', form)
  const adtBuildId = Form.useWatch('adtBuildId', form)

  // Parent-level linkages probe so the ADT radio card can display the "linking
  // required" warning before the user picks ADT. React-query dedupes the
  // identical key called from ADTCreateFields.
  const linkagesQuery = usePlaytesthubServiceAdminApi_GetAdtLinkages(sdk, {})
  const linkageCount = ((linkagesQuery.data?.linkages ?? []) as V1AdtLinkage[]).length

  const createMutation = usePlaytesthubServiceAdminApi_CreatePlaytestMutation(sdk, {
    onSuccess: () => {
      message.success('Playtest created')
      queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Playtests] })
      navigate('/')
    },
    onError: toastError('create')
  })

  const handleSubmit = (values: FormValues) => {
    const isADT = values.distributionModel === DistributionModel.ADT
    createMutation.mutate({
      data: {
        slug: values.slug,
        title: values.title,
        description: values.description,
        bannerImageUrl: values.bannerImageUrl,
        platforms: values.platforms,
        startsAt: values.dateRange?.[0].toISOString(),
        endsAt: values.dateRange?.[1].toISOString(),
        ndaRequired: values.ndaRequired,
        ndaText: values.ndaText,
        distributionModel: values.distributionModel,
        initialCodeQuantity: values.initialCodeQuantity,
        autoApprove: values.autoApprove ?? false,
        autoApproveLimit: values.autoApprove ? values.autoApproveLimit : undefined,
        adtNamespace: isADT ? values.adtNamespace : undefined,
        adtGameId: isADT ? values.adtGameId : undefined,
        adtBuildId: isADT ? values.adtBuildId : undefined
      }
    })
  }

  const sectionDivider = <div style={{ borderTop: '1px solid #f0f0f0' }} />

  return (
    <>
      <Typography.Title level={2} style={{ marginTop: 0 }}>
        Create New Playtest
      </Typography.Title>
      <Form<FormValues>
        form={form}
        layout="vertical"
        onFinish={handleSubmit}
        initialValues={{
          platforms: [],
          ndaRequired: false,
          distributionModel: DistributionModel.STEAM_KEYS,
          autoApprove: false
        }}>
        <Card
          styles={{ body: { padding: '0 32px' } }}
          style={{ borderRadius: 8 }}>
          <SectionRow title="Basic Information" description="General details about your playtest event.">
            <Form.Item label="Playtest Title" name="title" rules={[{ required: true, message: 'Title is required' }]}>
              <Input maxLength={200} placeholder="e.g. Starfield Alpha — Wave 2" />
            </Form.Item>
            <Form.Item
              label="Slug"
              name="slug"
              rules={[{ required: true, message: 'Slug is required' }]}
              extra="URL-safe identifier. Lowercase, numbers, hyphens. 3–64 characters.">
              <Input placeholder="e.g. starfield-alpha-w2" />
            </Form.Item>
            <Form.Item label="Description (optional)" name="description">
              <Input.TextArea rows={3} maxLength={10000} placeholder="Describe your playtest goals, what players should expect, etc." />
            </Form.Item>
            <Form.Item
              label="Banner Image URL (optional)"
              name="bannerImageUrl"
              rules={[bannerImageUrlRule]}
              extra="https only — backend rejects http."
              style={{ marginBottom: 0 }}>
              <Input placeholder="https://cdn.example.com/banner.jpg" data-testid="banner-image-url" />
            </Form.Item>
          </SectionRow>

          {sectionDivider}

          <SectionRow title="Schedule & Platforms" description="Set the playtest window and target platforms.">
            <Form.Item
              label={DATE_RANGE_LABEL}
              name="dateRange"
              extra={DATE_RANGE_HELP}
              rules={[dateRangeWindowRule]}
              getValueFromEvent={dateRangeUtcFromEvent}>
              <DatePicker.RangePicker showTime format="YYYY-MM-DD HH:mm" style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item
              label="Platforms"
              name="platforms"
              rules={[{ required: true, message: 'platforms must include at least one platform' }]}
              style={{ marginBottom: 0 }}>
              <PlatformsPills options={PLATFORMS} />
            </Form.Item>
          </SectionRow>

          {sectionDivider}

          <SectionRow
            title="Distribution Model"
            description="Choose how the game build or keys will be delivered to approved participants.">
            <Form.Item name="distributionModel" rules={[{ required: true }]} style={{ marginBottom: 0 }}>
              <DistributionRadioCards linkageCount={linkageCount} />
            </Form.Item>
            {distributionModel === DistributionModel.AGS_CAMPAIGN && (
              <Form.Item
                label="Initial code quantity"
                name="initialCodeQuantity"
                rules={[{ type: 'number', min: 1, max: 50000 }]}
                style={{ marginTop: 16, marginBottom: 0 }}>
                <InputNumber min={1} max={50000} style={{ width: '100%' }} />
              </Form.Item>
            )}
            {distributionModel === DistributionModel.ADT && (
              <div style={{ marginTop: 16 }}>
                <ADTCreateFields
                  form={form}
                  linkageId={adtLinkageId ?? ''}
                  adtGameId={adtGameId ?? ''}
                  adtBuildId={adtBuildId ?? ''}
                />
              </div>
            )}
          </SectionRow>

          {sectionDivider}

          <SectionRow title="Participant Approval" description="Configure how players are approved to join the playtest.">
            <Form.Item name="autoApprove" style={{ marginBottom: 0 }}>
              <ApprovalRadioCards />
            </Form.Item>
            {autoApprove && (
              <Form.Item
                label="Auto-approve limit"
                name="autoApproveLimit"
                dependencies={['autoApprove']}
                rules={[autoApproveLimitRule]}
                style={{ marginTop: 16, marginBottom: 0 }}>
                <InputNumber style={{ width: '100%' }} />
              </Form.Item>
            )}
          </SectionRow>

          {sectionDivider}

          <SectionRow
            title="NDA / Confidentiality"
            description="Optionally require players to accept a non-disclosure agreement before participating.">
            <Form.Item name="ndaRequired" valuePropName="checked" style={{ marginBottom: 0 }}>
              <Checkbox>Require NDA acceptance</Checkbox>
            </Form.Item>
            {ndaRequired && (
              <Form.Item
                label="NDA text"
                name="ndaText"
                rules={[{ required: true, message: 'NDA text required when NDA enabled' }]}
                style={{ marginTop: 16, marginBottom: 0 }}>
                <Input.TextArea rows={4} />
              </Form.Item>
            )}
          </SectionRow>
        </Card>

        {createMutation.isError && (
          <Alert
            type="error"
            style={{ marginTop: 16 }}
            message={createMutation.error?.response?.data?.errorMessage ?? 'Create failed'}
          />
        )}

        <div
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            padding: '16px 32px',
            marginTop: 16,
            background: '#fff',
            borderRadius: 8
          }}>
          <Button type="link" htmlType="button" onClick={() => navigate('/')} style={{ paddingInline: 0 }}>
            Cancel
          </Button>
          <Button type="primary" htmlType="submit" loading={createMutation.isPending}>
            Create
          </Button>
        </div>
      </Form>
    </>
  )
}

function PlaytestEditPage() {
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const location = useLocation()
  const queryClient = useQueryClient()
  const { playtestId = '' } = useParams()
  const [form] = Form.useForm<FormValues>()

  const { data, isLoading, error } = usePlaytesthubServiceAdminApi_GetPlaytest_ByPlaytestId(
    sdk,
    { playtestId },
    { enabled: !!playtestId }
  )

  const playtest = data?.playtest as V1Playtest | undefined

  const returnTo = (): string => {
    const from = (location.state as { from?: string } | null)?.from
    if (from) return from
    return playtest?.slug ? `/playtest/${playtest.slug}` : '/'
  }

  const editMutation = usePlaytesthubServiceAdminApi_PatchPlaytest_ByPlaytestIdMutation(sdk, {
    onSuccess: () => {
      message.success('Playtest updated')
      queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Playtests] })
      queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Playtest_ByPlaytestId] })
      navigate(returnTo())
    },
    onError: toastError('update')
  })

  const initialValues = useMemo<Partial<FormValues>>(() => {
    if (!playtest) return {}
    const start = playtest.startsAt ? dayjs.utc(playtest.startsAt) : undefined
    const end = playtest.endsAt ? dayjs.utc(playtest.endsAt) : undefined
    return {
      title: playtest.title ?? '',
      description: playtest.description ?? undefined,
      bannerImageUrl: playtest.bannerImageUrl ?? undefined,
      platforms: (playtest.platforms ?? []) as string[],
      dateRange: start && end ? [start, end] : undefined,
      ndaRequired: playtest.ndaRequired ?? false,
      ndaText: playtest.ndaText ?? undefined,
      distributionModel: (playtest.distributionModel as string | undefined) ?? DistributionModel.STEAM_KEYS,
      autoApprove: playtest.autoApprove ?? false,
      autoApproveLimit: playtest.autoApproveLimit ?? undefined
    }
  }, [playtest])

  useEffect(() => {
    if (playtest) form.setFieldsValue(initialValues as FormValues)
  }, [form, initialValues, playtest])

  const handleSubmit = (values: FormValues) => {
    editMutation.mutate({
      playtestId,
      data: {
        title: values.title,
        description: values.description,
        bannerImageUrl: values.bannerImageUrl,
        platforms: values.platforms,
        startsAt: values.dateRange?.[0].toISOString(),
        endsAt: values.dateRange?.[1].toISOString(),
        ndaRequired: values.ndaRequired,
        ndaText: values.ndaText,
        autoApprove: values.autoApprove ?? false,
        autoApproveLimit: values.autoApprove ? values.autoApproveLimit : undefined
      }
    })
  }

  if (isLoading) return <Spin description="Loading playtest..." />
  if (error) return <Alert type="error" message="Failed to load playtest." />
  if (!playtest) return <Alert type="warning" message="Playtest not found." />

  return (
    <>
      <Typography.Title level={2}>Edit playtest</Typography.Title>
      <Typography.Text type="secondary">
        Slug <code>{playtest.slug}</code> · distribution model <code>{playtest.distributionModel}</code> (immutable after creation).
      </Typography.Text>
      <Form<FormValues> form={form} layout="vertical" onFinish={handleSubmit} style={{ marginTop: 16 }} initialValues={initialValues as FormValues}>
        <Form.Item label="Title" name="title" rules={[{ required: true, message: 'Title is required' }]}>
          <Input maxLength={200} />
        </Form.Item>
        <Form.Item label="Description" name="description">
          <Input.TextArea rows={4} maxLength={10000} />
        </Form.Item>
        <Form.Item label="Banner image URL" name="bannerImageUrl" rules={[{ type: 'url', message: 'Must be a URL' }]}>
          <Input placeholder="https://..." />
        </Form.Item>
        <Form.Item label="Platforms" name="platforms" rules={[{ required: true, message: 'Pick at least one' }]}>
          <Select mode="multiple" options={PLATFORMS.map(p => ({ value: p.value, label: p.label }))} />
        </Form.Item>
        <Form.Item
          label={DATE_RANGE_LABEL}
          name="dateRange"
          extra={DATE_RANGE_HELP}
          rules={[dateRangeWindowRule]}
          getValueFromEvent={dateRangeUtcFromEvent}>
          <DatePicker.RangePicker showTime format="YYYY-MM-DD HH:mm" style={{ width: '100%' }} />
        </Form.Item>
        <Form.Item name="ndaRequired" valuePropName="checked">
          <Checkbox>Require NDA</Checkbox>
        </Form.Item>
        <Form.Item
          noStyle
          shouldUpdate={(prev: FormValues, next: FormValues) => prev.ndaRequired !== next.ndaRequired}>
          {({ getFieldValue }) =>
            getFieldValue('ndaRequired') && (
              <Form.Item label="NDA text" name="ndaText" rules={[{ required: true, message: 'NDA text required when NDA enabled' }]}>
                <Input.TextArea rows={4} />
              </Form.Item>
            )
          }
        </Form.Item>
        <Form.Item
          label="Auto-approve"
          name="autoApprove"
          valuePropName="checked"
          extra="Signups bypass the manual queue up to the cap below. Manual approve stays uncapped.">
          <Switch />
        </Form.Item>
        <Form.Item
          noStyle
          shouldUpdate={(prev: FormValues, next: FormValues) => prev.autoApprove !== next.autoApprove}>
          {({ getFieldValue }) =>
            getFieldValue('autoApprove') && (
              <Form.Item
                label="Auto-approve limit"
                name="autoApproveLimit"
                dependencies={['autoApprove']}
                rules={[autoApproveLimitRule]}>
                <InputNumber style={{ width: '100%' }} />
              </Form.Item>
            )
          }
        </Form.Item>
        {editMutation.isError && (
          <Form.Item>
            <Alert type="error" message={editMutation.error?.response?.data?.errorMessage ?? 'Update failed'} />
          </Form.Item>
        )}
        <Form.Item style={{ marginBottom: 0 }}>
          <Space>
            <Button onClick={() => navigate(returnTo())}>Cancel</Button>
            <Button type="primary" htmlType="submit" loading={editMutation.isPending}>
              Save
            </Button>
          </Space>
        </Form.Item>
      </Form>
    </>
  )
}
