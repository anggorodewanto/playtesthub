import { useAppUIContext } from '@accelbyte/sdk-extend-app-ui'
import { useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Checkbox,
  DatePicker,
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
  message
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
import { Route, Routes, useNavigate, useParams, useSearchParams } from 'react-router'
import { PlaytestDetailPage } from './PlaytestDetailPage'
import type { V1AdtBuild } from './playtesthubapi/generated-definitions/V1AdtBuild'
import type { V1AdtLinkage } from './playtesthubapi/generated-definitions/V1AdtLinkage'
import type { V1Playtest } from './playtesthubapi/generated-definitions/V1Playtest'
import type { V1WorkerHealthEntry } from './playtesthubapi/generated-definitions/V1WorkerHealthEntry'
import {
  Key_PlaytesthubServiceAdmin,
  usePlaytesthubServiceAdminApi_CreateAdtLinkagesCompleteMutation,
  usePlaytesthubServiceAdminApi_CreateAdtLinkagesStartMutation,
  usePlaytesthubServiceAdminApi_CreatePlaytestMutation,
  usePlaytesthubServiceAdminApi_CreatePlaytest_ByPlaytestIdTransitionStatuMutation,
  usePlaytesthubServiceAdminApi_DeleteAdtLinkage_ByAdtLinkageIdMutation,
  usePlaytesthubServiceAdminApi_DeletePlaytest_ByPlaytestIdMutation,
  usePlaytesthubServiceAdminApi_GetAdtLinkages,
  usePlaytesthubServiceAdminApi_GetBuildsAdt_ByAdtLinkageId,
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
import { toastError } from './shared/api-error'

const STATUS_TAG: Record<string, { text: string; color: string }> = {
  [PlaytestStatus.DRAFT]: { text: 'Draft', color: 'default' },
  [PlaytestStatus.OPEN]: { text: 'Open', color: 'green' },
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
  [DistributionModel.STEAM_KEYS]: 'Steam keys',
  [DistributionModel.AGS_CAMPAIGN]: 'AGS Campaign',
  [DistributionModel.ADT]: 'ADT'
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
    <div style={{ padding: 16 }}>
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
function ADTLinkCallbackPage() {
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const [error, setError] = useState<string | null>(null)

  const state = params.get('state') ?? ''
  const result = params.get('result') ?? ''
  const adtNamespace = params.get('adt_namespace') ?? ''

  const completeMutation = usePlaytesthubServiceAdminApi_CreateAdtLinkagesCompleteMutation(sdk, {
    onSuccess: () => {
      message.success('ADT namespace linked')
      navigate('/')
    },
    onError: (err: { message?: string }) => {
      setError(err.message ?? 'ADT linking failed')
    }
  })

  useEffect(() => {
    if (result === 'failed') {
      setError(params.get('reason') ?? 'ADT reported the link as failed')
      return
    }
    if (!state || !adtNamespace) {
      setError('Callback is missing the state or adt_namespace query parameter')
      return
    }
    completeMutation.mutate({ data: { state, adtNamespace } })
    // We deliberately depend only on the URL inputs — re-running on
    // mutation churn would refire the mutation.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state, adtNamespace, result])

  if (error) {
    return (
      <Space direction="vertical" style={{ width: '100%' }} data-testid="adt-link-callback">
        <Alert type="error" message="ADT linking failed" description={error} showIcon />
        <Button onClick={() => navigate('/')}>Back to playtests</Button>
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

  const columns = [
    { title: 'Slug', dataIndex: 'slug', key: 'slug' },
    { title: 'Title', dataIndex: 'title', key: 'title' },
    {
      title: 'Status',
      dataIndex: 'status',
      key: 'status',
      render: (_: unknown, row: V1Playtest) => <StatusTag status={row.status} startsAt={row.startsAt} endsAt={row.endsAt} />
    },
    {
      title: 'Distribution',
      dataIndex: 'distributionModel',
      key: 'distributionModel',
      render: (value: string | null | undefined) => DISTRIBUTION_LABEL[value ?? ''] ?? value ?? '—'
    },
    {
      title: 'Created',
      dataIndex: 'createdAt',
      key: 'createdAt',
      render: (value: string | null | undefined) => (value ? dayjs(value).format('YYYY-MM-DD HH:mm') : '—')
    },
    {
      title: 'Actions',
      key: 'actions',
      render: (_: unknown, row: V1Playtest) => {
        const isDraft = row.status === PlaytestStatus.DRAFT
        const isOpen = row.status === PlaytestStatus.OPEN
        return (
          <Space wrap>
            <Button size="small" type="primary" onClick={() => navigate(`playtest/${row.slug ?? ''}`)}>
              View
            </Button>
            <Button size="small" onClick={() => navigate(`${row.id}/edit`)}>
              Edit
            </Button>
            {isDraft && (
              <Popconfirm
                title="Publish this playtest?"
                description="Players will be able to see it and sign up."
                okText="Publish"
                onConfirm={() =>
                  transitionMutation.mutate({
                    playtestId: row.id ?? '',
                    data: { targetStatus: PlaytestStatus.OPEN }
                  })
                }>
                <Button size="small" type="primary">
                  Publish
                </Button>
              </Popconfirm>
            )}
            {isOpen && (
              <Popconfirm
                title="Close this playtest?"
                description="Players can no longer sign up. Existing applicants keep their state."
                okText="Close"
                okButtonProps={{ danger: true }}
                onConfirm={() =>
                  transitionMutation.mutate({
                    playtestId: row.id ?? '',
                    data: { targetStatus: PlaytestStatus.CLOSED }
                  })
                }>
                <Button size="small" danger>
                  Close
                </Button>
              </Popconfirm>
            )}
            <Popconfirm
              title="Soft-delete this playtest?"
              description="Row will be hidden from players. Applicants + codes are preserved."
              okText="Delete"
              okButtonProps={{ danger: true }}
              onConfirm={() => deleteMutation.mutate({ playtestId: row.id ?? '' })}>
              <Button size="small" danger>
                Delete
              </Button>
            </Popconfirm>
          </Space>
        )
      }
    }
  ]

  return (
    <>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <div>
          <Typography.Title level={2} style={{ margin: 0 }}>
            Playtests
          </Typography.Title>
          <Typography.Text type="secondary">Create, edit, and soft-delete playtests in this namespace.</Typography.Text>
        </div>
        <Space>
          <Button onClick={() => refetch()}>Refresh</Button>
          <Button type="primary" onClick={() => navigate('new')}>
            New playtest
          </Button>
        </Space>
      </div>

      {isLoading && <Spin description="Loading playtests..." />}
      {error && (
        <Alert
          type="error"
          message="Failed to load playtests."
          action={
            <Button size="small" onClick={() => refetch()}>
              Retry
            </Button>
          }
        />
      )}
      {!isLoading && !error && (
        <Table<V1Playtest>
          rowKey={row => row.id ?? row.slug ?? ''}
          dataSource={playtests}
          columns={columns}
          pagination={{ pageSize: 20 }}
        />
      )}
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
  adtFallbackDownloadUrl?: string
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

// ADTCreateFields renders the ADT-only fields inside PlaytestCreatePage
// when distribution_model = ADT. Pulls the studio's linkages + the
// build list for the picked linkage; falls back to a free-text Input
// when ADT's build endpoint is unavailable (STATUS_M5.md cut-if-behind).
function ADTCreateFields({
  form,
  linkageId,
  adtGameId
}: {
  form: ReturnType<typeof Form.useForm<FormValues>>[0]
  linkageId: string
  adtGameId: string
}) {
  const { sdk } = useAppUIContext()
  const linkagesQuery = usePlaytesthubServiceAdminApi_GetAdtLinkages(sdk, {})
  const buildsQuery = usePlaytesthubServiceAdminApi_GetBuildsAdt_ByAdtLinkageId(
    sdk,
    { adtLinkageId: linkageId, queryParams: { adtGameId } },
    { enabled: !!linkageId && !!adtGameId }
  )
  const linkages = (linkagesQuery.data?.linkages ?? []) as V1AdtLinkage[]
  const builds = (buildsQuery.data?.builds ?? []) as V1AdtBuild[]

  return (
    <>
      <Form.Item label="ADT linkage" name="adtLinkageId" rules={[{ required: true, message: 'Pick a linked ADT namespace' }]}>
        <Select
          placeholder="Select a linked ADT namespace"
          options={linkages.map(l => ({ value: l.id, label: `${l.adtNamespace ?? ''} (${l.studioNamespace ?? ''})` }))}
          onChange={(_id: string) => {
            const picked = linkages.find(l => l.id === _id)
            form.setFieldValue('adtNamespace', picked?.adtNamespace ?? '')
          }}
        />
      </Form.Item>
      <Form.Item label="ADT game id" name="adtGameId" rules={[{ required: true, message: 'ADT game id is required' }]}>
        <Input placeholder="e.g. mygame" />
      </Form.Item>
      <Form.Item label="ADT build id" name="adtBuildId" rules={[{ required: true, message: 'Pick a build' }]}>
        {builds.length > 0 ? (
          <Select
            placeholder="Pick a build"
            options={builds.map(b => ({ value: b.id, label: `${b.name ?? b.id} (${b.version ?? '—'})` }))}
          />
        ) : (
          <Input placeholder="Build id (paste from ADT Hub)" disabled={!linkageId || !adtGameId} />
        )}
      </Form.Item>
      <Form.Item
        label="Static fallback download URL"
        name="adtFallbackDownloadUrl"
        rules={[{ type: 'url', message: 'Must be a URL' }]}
        extra="Used when ADT cannot mint a per-applicant URL. https only.">
        <Input placeholder="https://..." />
      </Form.Item>
    </>
  )
}

function PlaytestCreatePage() {
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [form] = Form.useForm<FormValues>()

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
        adtBuildId: isADT ? values.adtBuildId : undefined,
        adtFallbackDownloadUrl: isADT ? values.adtFallbackDownloadUrl : undefined
      }
    })
  }

  return (
    <>
      <Typography.Title level={2}>New playtest</Typography.Title>
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
        <Form.Item label="Slug" name="slug" rules={[{ required: true, message: 'Slug is required' }]}>
          <Input placeholder="e.g. summer-alpha-2026" />
        </Form.Item>
        <Form.Item label="Title" name="title" rules={[{ required: true, message: 'Title is required' }]}>
          <Input maxLength={200} />
        </Form.Item>
        <Form.Item label="Description" name="description">
          <Input.TextArea rows={4} maxLength={10000} />
        </Form.Item>
        <Form.Item
          label="Banner image URL"
          name="bannerImageUrl"
          rules={[bannerImageUrlRule]}
          extra="https only — backend rejects http.">
          <Input placeholder="https://..." data-testid="banner-image-url" />
        </Form.Item>
        <Form.Item
          label="Platforms"
          name="platforms"
          rules={[{ required: true, message: 'platforms must include at least one platform' }]}>
          <Select mode="multiple" options={PLATFORMS.map(p => ({ value: p.value, label: p.label }))} data-testid="platforms-select" />
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
        <Form.Item label="Distribution model" name="distributionModel" rules={[{ required: true }]}>
          <Radio.Group>
            <Radio value={DistributionModel.STEAM_KEYS}>Steam keys</Radio>
            <Radio value={DistributionModel.AGS_CAMPAIGN}>AGS Campaign</Radio>
            <Radio value={DistributionModel.ADT}>ADT</Radio>
          </Radio.Group>
        </Form.Item>
        <Form.Item
          noStyle
          shouldUpdate={(prev: FormValues, next: FormValues) => prev.distributionModel !== next.distributionModel}>
          {({ getFieldValue }) =>
            getFieldValue('distributionModel') === DistributionModel.AGS_CAMPAIGN && (
              <Form.Item label="Initial code quantity" name="initialCodeQuantity" rules={[{ type: 'number', min: 1, max: 50000 }]}>
                <InputNumber min={1} max={50000} style={{ width: '100%' }} />
              </Form.Item>
            )
          }
        </Form.Item>
        <Form.Item
          noStyle
          shouldUpdate={(prev: FormValues, next: FormValues) =>
            prev.distributionModel !== next.distributionModel ||
            prev.adtLinkageId !== next.adtLinkageId ||
            prev.adtGameId !== next.adtGameId
          }>
          {({ getFieldValue }) =>
            getFieldValue('distributionModel') === DistributionModel.ADT && (
              <ADTCreateFields form={form} linkageId={getFieldValue('adtLinkageId') ?? ''} adtGameId={getFieldValue('adtGameId') ?? ''} />
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
        {createMutation.isError && (
          <Form.Item>
            <Alert type="error" message={createMutation.error?.response?.data?.errorMessage ?? 'Create failed'} />
          </Form.Item>
        )}
        <Form.Item style={{ marginBottom: 0 }}>
          <Space>
            <Button onClick={() => navigate('/')}>Cancel</Button>
            <Button type="primary" htmlType="submit" loading={createMutation.isPending}>
              Create
            </Button>
          </Space>
        </Form.Item>
      </Form>
    </>
  )
}

function PlaytestEditPage() {
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { playtestId = '' } = useParams()
  const [form] = Form.useForm<FormValues>()

  const { data, isLoading, error } = usePlaytesthubServiceAdminApi_GetPlaytest_ByPlaytestId(
    sdk,
    { playtestId },
    { enabled: !!playtestId }
  )

  const editMutation = usePlaytesthubServiceAdminApi_PatchPlaytest_ByPlaytestIdMutation(sdk, {
    onSuccess: () => {
      message.success('Playtest updated')
      queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Playtests] })
      queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Playtest_ByPlaytestId] })
      navigate('/')
    },
    onError: toastError('update')
  })

  const playtest = data?.playtest as V1Playtest | undefined

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
            <Button onClick={() => navigate('/')}>Cancel</Button>
            <Button type="primary" htmlType="submit" loading={editMutation.isPending}>
              Save
            </Button>
          </Space>
        </Form.Item>
      </Form>
    </>
  )
}
