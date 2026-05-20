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
  Statistic,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
  Upload,
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
import type { V1Applicant } from './playtesthubapi/generated-definitions/V1Applicant'
import type { V1AuditLogEntry } from './playtesthubapi/generated-definitions/V1AuditLogEntry'
import type { V1Code } from './playtesthubapi/generated-definitions/V1Code'
import type { V1CodePoolStats } from './playtesthubapi/generated-definitions/V1CodePoolStats'
import type { V1AdtBuild } from './playtesthubapi/generated-definitions/V1AdtBuild'
import type { V1AdtLinkage } from './playtesthubapi/generated-definitions/V1AdtLinkage'
import type { V1MultiChoiceOption } from './playtesthubapi/generated-definitions/V1MultiChoiceOption'
import type { V1Playtest } from './playtesthubapi/generated-definitions/V1Playtest'
import type { V1Survey } from './playtesthubapi/generated-definitions/V1Survey'
import type { V1SurveyAnswer } from './playtesthubapi/generated-definitions/V1SurveyAnswer'
import type { V1SurveyQuestion } from './playtesthubapi/generated-definitions/V1SurveyQuestion'
import type { V1SurveyResponse } from './playtesthubapi/generated-definitions/V1SurveyResponse'
import type { V1UploadCodesRejection } from './playtesthubapi/generated-definitions/V1UploadCodesRejection'
import type { V1WorkerHealthEntry } from './playtesthubapi/generated-definitions/V1WorkerHealthEntry'
import {
  Key_PlaytesthubServiceAdmin,
  usePlaytesthubServiceAdminApi_CreateAdtLinkagesCompleteMutation,
  usePlaytesthubServiceAdminApi_CreateAdtLinkagesStartMutation,
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdApproveMutation,
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRejectMutation,
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRetryDmMutation,
  usePlaytesthubServiceAdminApi_CreateCodesSyncFromAg_ByPlaytestIdMutation,
  usePlaytesthubServiceAdminApi_CreateCodesTopUp_ByPlaytestIdMutation,
  usePlaytesthubServiceAdminApi_CreateCodesUpload_ByPlaytestIdMutation,
  usePlaytesthubServiceAdminApi_CreatePlaytestMutation,
  usePlaytesthubServiceAdminApi_CreatePlaytest_ByPlaytestIdTransitionStatuMutation,
  usePlaytesthubServiceAdminApi_CreateSurvey_ByPlaytestIdMutation,
  usePlaytesthubServiceAdminApi_DeleteAdtLinkage_ByAdtLinkageIdMutation,
  usePlaytesthubServiceAdminApi_DeletePlaytest_ByPlaytestIdMutation,
  usePlaytesthubServiceAdminApi_GetAdtLinkages,
  usePlaytesthubServiceAdminApi_GetApplicants_ByPlaytestId,
  usePlaytesthubServiceAdminApi_GetAuditLog_ByPlaytestId,
  usePlaytesthubServiceAdminApi_GetBuildsAdt_ByAdtLinkageId,
  usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId,
  usePlaytesthubServiceAdminApi_GetPlaytest_ByPlaytestId,
  usePlaytesthubServiceAdminApi_GetPlaytests,
  usePlaytesthubServiceAdminApi_GetSurveyResponses_ByPlaytestId,
  usePlaytesthubServiceAdminApi_GetWorkersHealth,
  usePlaytesthubServiceAdminApi_PatchPlaytest_ByPlaytestIdMutation,
  usePlaytesthubServiceAdminApi_PatchSurvey_ByPlaytestIdMutation
} from './playtesthubapi/generated-admin/queries/PlaytesthubServiceAdmin.query'
import { usePlaytesthubServiceApi_GetSurveyPlayer_ByPlaytestId } from './playtesthubapi/generated-public/queries/PlaytesthubService.query'

const PLATFORMS = [
  { value: 'PLATFORM_STEAM', label: 'Steam' },
  { value: 'PLATFORM_XBOX', label: 'Xbox' },
  { value: 'PLATFORM_PLAYSTATION', label: 'PlayStation' },
  { value: 'PLATFORM_EPIC', label: 'Epic' },
  { value: 'PLATFORM_OTHER', label: 'Other' }
] as const

// Mirror proto enums (proto/playtesthub/v1/playtesthub.proto) here
// because @accelbyte/codegen emits z.any() for every enum, leaving no
// generated consts to import. Keep these in lockstep with the .proto.
const PlaytestStatus = {
  UNSPECIFIED: 'PLAYTEST_STATUS_UNSPECIFIED',
  DRAFT: 'PLAYTEST_STATUS_DRAFT',
  OPEN: 'PLAYTEST_STATUS_OPEN',
  CLOSED: 'PLAYTEST_STATUS_CLOSED'
} as const

const ApplicantStatus = {
  UNSPECIFIED: 'APPLICANT_STATUS_UNSPECIFIED',
  PENDING: 'APPLICANT_STATUS_PENDING',
  APPROVED: 'APPLICANT_STATUS_APPROVED',
  REJECTED: 'APPLICANT_STATUS_REJECTED'
} as const
type ApplicantStatusValue = (typeof ApplicantStatus)[keyof typeof ApplicantStatus]

const DmStatus = {
  UNSPECIFIED: 'DM_STATUS_UNSPECIFIED',
  SENT: 'DM_STATUS_SENT',
  FAILED: 'DM_STATUS_FAILED'
} as const

const DistributionModel = {
  UNSPECIFIED: 'DISTRIBUTION_MODEL_UNSPECIFIED',
  STEAM_KEYS: 'DISTRIBUTION_MODEL_STEAM_KEYS',
  AGS_CAMPAIGN: 'DISTRIBUTION_MODEL_AGS_CAMPAIGN',
  ADT: 'DISTRIBUTION_MODEL_ADT'
} as const

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

// toastError builds the standard mutation onError handler. The verb is
// the second half of the fallback message ("Failed to <verb>"); the
// gateway's errorMessage from gRPC status takes precedence when present.
type ApiError = { response?: { data?: { errorMessage?: string } } }
function toastError(verb: string) {
  return (err: ApiError) => message.error(err?.response?.data?.errorMessage ?? `Failed to ${verb}`)
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
        <Route path=":playtestId/applicants" element={<ApplicantsPage />} />
        <Route path=":playtestId/codes" element={<CodePoolPage />} />
        <Route path=":playtestId/survey" element={<SurveyBuilderPage />} />
        <Route path=":playtestId/survey/responses" element={<SurveyResponsesPage />} />
        <Route path=":playtestId/audit" element={<AuditLogPage />} />
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
            <Button size="small" onClick={() => navigate(`${row.id}/applicants`)}>
              Applicants
            </Button>
            <Button size="small" onClick={() => navigate(`${row.id}/codes`)}>
              Codes
            </Button>
            <Button size="small" onClick={() => navigate(`${row.id}/survey`)}>
              Survey
            </Button>
            <Button size="small" onClick={() => navigate(`${row.id}/survey/responses`)}>
              Responses
            </Button>
            <Button size="small" onClick={() => navigate(`${row.id}/audit`)}>
              Audit
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
          rules={[{ type: 'url', message: 'Must be a URL' }]}
          extra="https only — backend rejects http.">
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

const APPLICANT_STATUS_TAG: Record<string, { text: string; color: string }> = {
  [ApplicantStatus.PENDING]: { text: 'Pending', color: 'gold' },
  [ApplicantStatus.APPROVED]: { text: 'Approved', color: 'green' },
  [ApplicantStatus.REJECTED]: { text: 'Rejected', color: 'red' }
}

const DM_STATUS_TAG: Record<string, { text: string; color: string }> = {
  DM_STATUS_PENDING: { text: 'Pending', color: 'default' },
  [DmStatus.SENT]: { text: 'Sent', color: 'green' },
  [DmStatus.FAILED]: { text: 'Failed', color: 'red' }
}

const POOL_LOW_RATIO = 0.1

function isPoolLow(stats: V1CodePoolStats | null | undefined): boolean {
  const total = stats?.total ?? 0
  const unused = stats?.unused ?? 0
  if (total <= 0) return false
  return unused / total <= POOL_LOW_RATIO
}

function LowPoolBanner({ stats }: { stats: V1CodePoolStats | null | undefined }) {
  if (!isPoolLow(stats)) return null
  return (
    <Alert
      type="warning"
      showIcon
      message="Code pool is low"
      description={`Only ${stats?.unused ?? 0} of ${stats?.total ?? 0} codes remain unused. Top up before approving more applicants.`}
      style={{ marginBottom: 16 }}
    />
  )
}

function ApplicantsPage() {
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { playtestId = '' } = useParams()

  const [statusFilter, setStatusFilter] = useState<string>(ApplicantStatus.UNSPECIFIED)
  const [dmFailedOnly, setDmFailedOnly] = useState(false)
  const [rejectTarget, setRejectTarget] = useState<V1Applicant | null>(null)
  const [rejectReason, setRejectReason] = useState('')

  const playtestQuery = usePlaytesthubServiceAdminApi_GetPlaytest_ByPlaytestId(
    sdk,
    { playtestId },
    { enabled: !!playtestId }
  )

  const applicantsQuery = usePlaytesthubServiceAdminApi_GetApplicants_ByPlaytestId(
    sdk,
    {
      playtestId,
      queryParams: {
        statusFilter: statusFilter as ApplicantStatusValue,
        dmFailedFilter: dmFailedOnly
      }
    },
    { enabled: !!playtestId }
  )

  const codesQuery = usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId(
    sdk,
    { playtestId },
    { enabled: !!playtestId }
  )

  const invalidateApplicants = () => {
    queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Applicants_ByPlaytestId] })
    queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Codes_ByPlaytestId] })
  }

  const approveMutation = usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdApproveMutation(sdk, {
    onSuccess: () => {
      message.success('Applicant approved')
      invalidateApplicants()
    },
    onError: toastError('approve')
  })

  const rejectMutation = usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRejectMutation(sdk, {
    onSuccess: () => {
      message.success('Applicant rejected')
      setRejectTarget(null)
      setRejectReason('')
      invalidateApplicants()
    },
    onError: toastError('reject')
  })

  const retryDmMutation = usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRetryDmMutation(sdk, {
    onSuccess: () => {
      message.success('Retry DM enqueued')
      invalidateApplicants()
    },
    onError: toastError('retry DM')
  })

  const playtest = playtestQuery.data?.playtest as V1Playtest | undefined
  const applicants = (applicantsQuery.data?.applicants ?? []) as V1Applicant[]
  const stats = codesQuery.data?.stats

  const columns = [
    { title: 'Discord', dataIndex: 'discordHandle', key: 'discordHandle', render: (v: string | null | undefined) => v ?? '—' },
    {
      title: 'Platforms',
      dataIndex: 'platforms',
      key: 'platforms',
      render: (v: string[] | null | undefined) => (v ?? []).map(p => p.replace('PLATFORM_', '').toLowerCase()).join(', ') || '—'
    },
    {
      title: 'Status',
      dataIndex: 'status',
      key: 'status',
      render: (v: string | null | undefined) => {
        const info = APPLICANT_STATUS_TAG[v ?? ''] ?? { text: v ?? '—', color: 'default' }
        return <Tag color={info.color}>{info.text}</Tag>
      }
    },
    {
      title: 'DM',
      dataIndex: 'lastDmStatus',
      key: 'lastDmStatus',
      render: (v: string | null | undefined, row: V1Applicant) => {
        if (!v) return <Tag>—</Tag>
        const info = DM_STATUS_TAG[v] ?? { text: v, color: 'default' }
        return (
          <Tag color={info.color} title={row.lastDmError ?? undefined}>
            {info.text}
          </Tag>
        )
      }
    },
    {
      title: 'Created',
      dataIndex: 'createdAt',
      key: 'createdAt',
      render: (v: string | null | undefined) => (v ? dayjs(v).format('YYYY-MM-DD HH:mm') : '—')
    },
    {
      title: 'Actions',
      key: 'actions',
      render: (_: unknown, row: V1Applicant) => {
        const isPending = row.status === ApplicantStatus.PENDING
        const canRetryDm = row.status === ApplicantStatus.APPROVED && row.lastDmStatus === DmStatus.FAILED
        return (
          <Space wrap>
            <Popconfirm
              title="Approve this applicant?"
              description="A code will be reserved and granted from the pool."
              okText="Approve"
              disabled={!isPending}
              onConfirm={() => approveMutation.mutate({ applicantId: row.id ?? '', data: {} })}>
              <Button size="small" type="primary" disabled={!isPending}>
                Approve
              </Button>
            </Popconfirm>
            <Button size="small" danger disabled={!isPending} onClick={() => setRejectTarget(row)}>
              Reject
            </Button>
            {canRetryDm && (
              <Button size="small" onClick={() => retryDmMutation.mutate({ applicantId: row.id ?? '', data: {} })}>
                Retry DM
              </Button>
            )}
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
            Applicants
          </Typography.Title>
          <Typography.Text type="secondary">
            {playtest?.title ? `${playtest.title} (${playtest.slug})` : 'Loading playtest…'}
          </Typography.Text>
        </div>
        <Space>
          <Button onClick={() => navigate('/')}>Back</Button>
          <Button onClick={() => applicantsQuery.refetch()}>Refresh</Button>
        </Space>
      </div>

      <LowPoolBanner stats={stats} />

      <Space style={{ marginBottom: 16 }} wrap>
        <Select
          aria-label="Status filter"
          value={statusFilter}
          onChange={setStatusFilter}
          style={{ width: 200 }}
          options={[
            { value: ApplicantStatus.UNSPECIFIED, label: 'All statuses' },
            { value: ApplicantStatus.PENDING, label: 'Pending' },
            { value: ApplicantStatus.APPROVED, label: 'Approved' },
            { value: ApplicantStatus.REJECTED, label: 'Rejected' }
          ]}
        />
        <Checkbox checked={dmFailedOnly} onChange={e => setDmFailedOnly(e.target.checked)}>
          DM failed only
        </Checkbox>
      </Space>

      {applicantsQuery.isLoading && <Spin description="Loading applicants..." />}
      {applicantsQuery.error && (
        <Alert
          type="error"
          message="Failed to load applicants."
          action={
            <Button size="small" onClick={() => applicantsQuery.refetch()}>
              Retry
            </Button>
          }
        />
      )}
      {!applicantsQuery.isLoading && !applicantsQuery.error && (
        <Table<V1Applicant>
          rowKey={row => row.id ?? ''}
          dataSource={applicants}
          columns={columns}
          pagination={{ pageSize: 50 }}
        />
      )}

      <Modal
        title="Reject applicant"
        open={!!rejectTarget}
        onCancel={() => {
          setRejectTarget(null)
          setRejectReason('')
        }}
        onOk={() =>
          rejectTarget &&
          rejectMutation.mutate({
            applicantId: rejectTarget.id ?? '',
            data: { rejectionReason: rejectReason.trim() || undefined }
          })
        }
        okText="Reject"
        okButtonProps={{ danger: true, loading: rejectMutation.isPending }}>
        <Typography.Paragraph>
          Reject <strong>{rejectTarget?.discordHandle ?? rejectTarget?.userId ?? 'applicant'}</strong>? This is terminal — they cannot be re-approved.
        </Typography.Paragraph>
        <Form.Item label="Reason (optional)" style={{ marginBottom: 0 }}>
          <Input.TextArea
            rows={3}
            value={rejectReason}
            maxLength={500}
            onChange={e => setRejectReason(e.target.value)}
            placeholder="Stored on the applicant row for your future reference. Not shown to the player."
          />
        </Form.Item>
      </Modal>
    </>
  )
}

const CODE_STATE_TAG: Record<string, { text: string; color: string }> = {
  CODE_STATE_UNUSED: { text: 'Unused', color: 'default' },
  CODE_STATE_RESERVED: { text: 'Reserved', color: 'gold' },
  CODE_STATE_GRANTED: { text: 'Granted', color: 'green' }
}

function CodePoolPage() {
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { playtestId = '' } = useParams()

  const [csvText, setCsvText] = useState('')
  const [csvFilename, setCsvFilename] = useState('')
  const [topUpQty, setTopUpQty] = useState<number | null>(100)
  const [rejections, setRejections] = useState<V1UploadCodesRejection[]>([])

  const playtestQuery = usePlaytesthubServiceAdminApi_GetPlaytest_ByPlaytestId(
    sdk,
    { playtestId },
    { enabled: !!playtestId }
  )
  const codesQuery = usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId(sdk, { playtestId }, { enabled: !!playtestId })

  const invalidateCodes = () => queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Codes_ByPlaytestId] })

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

  const playtest = playtestQuery.data?.playtest as V1Playtest | undefined
  const stats = codesQuery.data?.stats
  const codes = (codesQuery.data?.codes ?? []) as V1Code[]
  const isAGS = playtest?.distributionModel === DistributionModel.AGS_CAMPAIGN

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

  const codeColumns = [
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

  return (
    <>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <div>
          <Typography.Title level={2} style={{ margin: 0 }}>
            Code pool
          </Typography.Title>
          <Typography.Text type="secondary">
            {playtest?.title ? `${playtest.title} (${playtest.slug})` : 'Loading playtest…'}
            {playtest && (
              <>
                {' · '}
                <code>{playtest.distributionModel}</code>
              </>
            )}
          </Typography.Text>
        </div>
        <Space>
          <Button onClick={() => navigate('/')}>Back</Button>
          <Button onClick={() => codesQuery.refetch()}>Refresh</Button>
        </Space>
      </div>

      <LowPoolBanner stats={stats} />

      <div style={{ display: 'flex', gap: 24, marginBottom: 24, flexWrap: 'wrap' }}>
        <Statistic title="Total" value={stats?.total ?? 0} />
        <Statistic title="Unused" value={stats?.unused ?? 0} />
        <Statistic title="Reserved" value={stats?.reserved ?? 0} />
        <Statistic title="Granted" value={stats?.granted ?? 0} />
      </div>

      {!isAGS && (
        <div style={{ marginBottom: 24 }}>
          <Typography.Title level={4}>Upload Steam keys</Typography.Title>
          <Typography.Paragraph type="secondary">
            One code per line. UTF-8, max 10 MB, max 50,000 lines, charset <code>[A-Za-z0-9._-]</code>, length 1–128. Any
            invalid line rejects the whole file.
          </Typography.Paragraph>
          <Upload accept=".csv,.txt,text/plain,text/csv" beforeUpload={handleFileChosen} maxCount={1} showUploadList={false}>
            <Button>Choose file</Button>
          </Upload>
          {csvFilename && (
            <Typography.Paragraph style={{ marginTop: 8 }}>
              Selected: <code>{csvFilename}</code>
            </Typography.Paragraph>
          )}
          <Button
            type="primary"
            disabled={!csvText}
            loading={uploadMutation.isPending}
            style={{ marginTop: 8 }}
            onClick={() => uploadMutation.mutate({ playtestId, data: { csvContent: csvText, filename: csvFilename || undefined } })}>
            Upload
          </Button>
          {rejections.length > 0 && (
            <Alert
              type="error"
              style={{ marginTop: 12 }}
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
        </div>
      )}

      {isAGS && (
        <div style={{ marginBottom: 24 }}>
          <Typography.Title level={4}>Generate / sync AGS Campaign codes</Typography.Title>
          <Typography.Paragraph type="secondary">
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
        </div>
      )}

      <Typography.Title level={4}>Codes</Typography.Title>
      {codesQuery.isLoading && <Spin description="Loading codes..." />}
      {codesQuery.error && (
        <Alert
          type="error"
          message="Failed to load codes."
          action={
            <Button size="small" onClick={() => codesQuery.refetch()}>
              Retry
            </Button>
          }
        />
      )}
      {!codesQuery.isLoading && !codesQuery.error && (
        <Table<V1Code> rowKey={row => row.id ?? ''} dataSource={codes} columns={codeColumns} pagination={{ pageSize: 50 }} />
      )}
    </>
  )
}

const QUESTION_TYPE_TEXT = 'SURVEY_QUESTION_TYPE_TEXT'
const QUESTION_TYPE_RATING = 'SURVEY_QUESTION_TYPE_RATING'
const QUESTION_TYPE_MULTI_CHOICE = 'SURVEY_QUESTION_TYPE_MULTI_CHOICE'
const QUESTION_TYPE_LABEL: Record<string, string> = {
  [QUESTION_TYPE_TEXT]: 'Text',
  [QUESTION_TYPE_RATING]: 'Rating (1–5)',
  [QUESTION_TYPE_MULTI_CHOICE]: 'Multi-choice'
}
const MAX_QUESTIONS = 50
const MAX_PROMPT = 1000
const MIN_OPTIONS = 2
const MAX_OPTIONS = 20
const MAX_OPTION_LABEL = 200

type DraftOption = { id?: string; label: string }
type DraftQuestion = {
  key: string
  id?: string
  type: string
  prompt: string
  required: boolean
  allowMultiple: boolean
  options: DraftOption[]
}

let draftKeyCounter = 0
const nextDraftKey = (): string => {
  draftKeyCounter += 1
  return `q-${draftKeyCounter}-${Date.now()}`
}

function questionToDraft(q: V1SurveyQuestion): DraftQuestion {
  return {
    key: nextDraftKey(),
    id: q.id ?? undefined,
    type: typeof q.type === 'string' ? q.type : QUESTION_TYPE_TEXT,
    prompt: q.prompt ?? '',
    required: q.required ?? false,
    allowMultiple: q.allowMultiple ?? false,
    options: (q.options ?? []).map(o => ({ id: o.id ?? undefined, label: o.label ?? '' }))
  }
}

function draftToWire(q: DraftQuestion): V1SurveyQuestion {
  const base: V1SurveyQuestion = {
    type: q.type,
    prompt: q.prompt,
    required: q.required
  }
  if (q.id) base.id = q.id
  if (q.type === QUESTION_TYPE_MULTI_CHOICE) {
    base.allowMultiple = q.allowMultiple
    base.options = q.options.map<V1MultiChoiceOption>(o => (o.id ? { id: o.id, label: o.label } : { label: o.label }))
  }
  return base
}

function freshTextQuestion(): DraftQuestion {
  return { key: nextDraftKey(), type: QUESTION_TYPE_TEXT, prompt: '', required: false, allowMultiple: false, options: [] }
}

function validateDraft(questions: DraftQuestion[]): string | null {
  if (questions.length === 0) return 'Add at least one question'
  if (questions.length > MAX_QUESTIONS) return `At most ${MAX_QUESTIONS} questions`
  for (const [i, q] of questions.entries()) {
    const label = `Question ${i + 1}`
    if (!q.prompt.trim()) return `${label}: prompt is required`
    if (q.prompt.length > MAX_PROMPT) return `${label}: prompt exceeds ${MAX_PROMPT} chars`
    if (q.type === QUESTION_TYPE_MULTI_CHOICE) {
      if (q.options.length < MIN_OPTIONS || q.options.length > MAX_OPTIONS) {
        return `${label}: multi-choice needs ${MIN_OPTIONS}–${MAX_OPTIONS} options`
      }
      for (const [j, opt] of q.options.entries()) {
        if (!opt.label.trim()) return `${label} option ${j + 1}: label is required`
        if (opt.label.length > MAX_OPTION_LABEL) return `${label} option ${j + 1}: label exceeds ${MAX_OPTION_LABEL} chars`
      }
    }
  }
  return null
}

function SurveyBuilderPage() {
  const { sdk } = useAppUIContext()
  const { playtestId = '' } = useParams<{ playtestId: string }>()

  const playtestQuery = usePlaytesthubServiceAdminApi_GetPlaytest_ByPlaytestId(sdk, { playtestId })
  const playtest = playtestQuery.data?.playtest as V1Playtest | undefined
  const hasSurvey = Boolean(playtest?.surveyId)

  // Player GetSurvey is the authoritative read path (no admin GET in proto).
  // Returns NotFound for DRAFT playtests — render the warning + blank form in
  // that case so first-version edits still work.
  const surveyQuery = usePlaytesthubServiceApi_GetSurveyPlayer_ByPlaytestId(
    sdk,
    { playtestId },
    { enabled: hasSurvey, retry: false }
  )

  if (playtestQuery.isLoading) return <Spin description="Loading playtest..." />
  if (playtestQuery.error || !playtest) return <Alert type="error" message="Failed to load playtest." />
  if (hasSurvey && surveyQuery.isLoading) return <Spin description="Loading existing survey..." />

  const initialSurvey = (hasSurvey && surveyQuery.data?.survey ? surveyQuery.data.survey : null) as V1Survey | null
  const draftPreloadFailed = hasSurvey && surveyQuery.isError && playtest.status === PlaytestStatus.DRAFT
  // Mounting a fresh form per data shape avoids the cascading-effect anti-pattern.
  const formKey = `${playtestId}-${initialSurvey?.id ?? 'new'}-${draftPreloadFailed ? 'draft-blank' : 'ok'}`

  return (
    <SurveyBuilderForm
      key={formKey}
      playtestId={playtestId}
      playtest={playtest}
      initialSurvey={initialSurvey}
      hasSurvey={hasSurvey}
      draftPreloadFailed={draftPreloadFailed}
    />
  )
}

type SurveyBuilderFormProps = {
  playtestId: string
  playtest: V1Playtest
  initialSurvey: V1Survey | null
  hasSurvey: boolean
  draftPreloadFailed: boolean
}

function SurveyBuilderForm({ playtestId, playtest, initialSurvey, hasSurvey, draftPreloadFailed }: SurveyBuilderFormProps) {
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const [questions, setQuestions] = useState<DraftQuestion[]>(() => {
    if (initialSurvey?.questions?.length) return initialSurvey.questions.map(questionToDraft)
    return [freshTextQuestion()]
  })
  const version = initialSurvey?.version ?? null

  const createMutation = usePlaytesthubServiceAdminApi_CreateSurvey_ByPlaytestIdMutation(sdk, {
    onSuccess: () => {
      message.success('Survey created')
      queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Playtest_ByPlaytestId] })
      navigate('/')
    },
    onError: toastError('create survey')
  })
  const editMutation = usePlaytesthubServiceAdminApi_PatchSurvey_ByPlaytestIdMutation(sdk, {
    onSuccess: () => {
      message.success('Survey updated (new version)')
      queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Playtest_ByPlaytestId] })
      queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Survey_ByPlaytestId] })
      navigate('/')
    },
    onError: toastError('update survey')
  })

  const updateQuestion = (key: string, patch: Partial<DraftQuestion>) => {
    setQuestions(prev => prev.map(q => (q.key === key ? { ...q, ...patch } : q)))
  }
  const moveQuestion = (key: string, direction: -1 | 1) => {
    setQuestions(prev => {
      const idx = prev.findIndex(q => q.key === key)
      if (idx < 0) return prev
      const target = idx + direction
      if (target < 0 || target >= prev.length) return prev
      const next = prev.slice()
      const tmp = next[idx]
      next[idx] = next[target]
      next[target] = tmp
      return next
    })
  }
  const removeQuestion = (key: string) => setQuestions(prev => prev.filter(q => q.key !== key))
  const addQuestion = () => setQuestions(prev => [...prev, freshTextQuestion()])
  const setQuestionType = (key: string, type: string) =>
    updateQuestion(key, {
      type,
      options: type === QUESTION_TYPE_MULTI_CHOICE ? [{ label: '' }, { label: '' }] : [],
      allowMultiple: type === QUESTION_TYPE_MULTI_CHOICE ? false : false
    })
  const updateOption = (qKey: string, oIndex: number, label: string) => {
    setQuestions(prev =>
      prev.map(q => {
        if (q.key !== qKey) return q
        const next = q.options.slice()
        next[oIndex] = { ...next[oIndex], label }
        return { ...q, options: next }
      })
    )
  }
  const addOption = (qKey: string) => {
    setQuestions(prev =>
      prev.map(q => (q.key === qKey && q.options.length < MAX_OPTIONS ? { ...q, options: [...q.options, { label: '' }] } : q))
    )
  }
  const removeOption = (qKey: string, oIndex: number) => {
    setQuestions(prev =>
      prev.map(q => (q.key === qKey && q.options.length > MIN_OPTIONS ? { ...q, options: q.options.filter((_, i) => i !== oIndex) } : q))
    )
  }

  const onSave = () => {
    const error = validateDraft(questions)
    if (error) {
      message.error(error)
      return
    }
    const wireQuestions = questions.map(draftToWire)
    if (hasSurvey) {
      editMutation.mutate({ playtestId, data: { questions: wireQuestions } })
      return
    }
    createMutation.mutate({ playtestId, data: { questions: wireQuestions } })
  }

  const saving = createMutation.isPending || editMutation.isPending

  return (
    <>
      <div style={{ marginBottom: 16 }}>
        <Typography.Title level={3} style={{ margin: 0 }}>
          Survey — {playtest.title ?? playtest.slug}
        </Typography.Title>
        <Typography.Text type="secondary">
          {hasSurvey ? `Editing existing survey` : 'Configure the post-playtest survey for approved players.'}
          {version != null && ` · current version v${version} (saving creates v${version + 1})`}
        </Typography.Text>
      </div>

      {draftPreloadFailed && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 16 }}
          message="DRAFT playtest survey can't be previewed"
          description="Loading existing survey questions requires the playtest to be OPEN. Saving here will create a new version that won't preserve question/option ids — only safe before any responses exist."
        />
      )}

      <Space direction="vertical" size="middle" style={{ display: 'flex' }}>
        {questions.map((q, i) => (
          <div
            key={q.key}
            data-testid="survey-question"
            style={{ border: '1px solid #d9d9d9', borderRadius: 6, padding: 16 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
              <Typography.Text strong>Question {i + 1}</Typography.Text>
              <Space size={4}>
                <Button size="small" onClick={() => moveQuestion(q.key, -1)} disabled={i === 0} aria-label={`Move question ${i + 1} up`}>
                  ↑
                </Button>
                <Button
                  size="small"
                  onClick={() => moveQuestion(q.key, 1)}
                  disabled={i === questions.length - 1}
                  aria-label={`Move question ${i + 1} down`}>
                  ↓
                </Button>
                <Popconfirm title="Remove this question?" okText="Remove" okButtonProps={{ danger: true }} onConfirm={() => removeQuestion(q.key)}>
                  <Button size="small" danger aria-label={`Remove question ${i + 1}`}>
                    Remove
                  </Button>
                </Popconfirm>
              </Space>
            </div>
            <Form layout="vertical">
              <Form.Item label="Type">
                <Select
                  value={q.type}
                  onChange={val => setQuestionType(q.key, val)}
                  options={Object.entries(QUESTION_TYPE_LABEL).map(([value, label]) => ({ value, label }))}
                />
              </Form.Item>
              <Form.Item label="Prompt">
                <Input.TextArea
                  value={q.prompt}
                  maxLength={MAX_PROMPT}
                  showCount
                  onChange={e => updateQuestion(q.key, { prompt: e.target.value })}
                  rows={2}
                  placeholder="What did you think of the build?"
                />
              </Form.Item>
              <Form.Item>
                <Checkbox checked={q.required} onChange={e => updateQuestion(q.key, { required: e.target.checked })}>
                  Required
                </Checkbox>
              </Form.Item>
              {q.type === QUESTION_TYPE_MULTI_CHOICE && (
                <>
                  <Form.Item>
                    <Checkbox checked={q.allowMultiple} onChange={e => updateQuestion(q.key, { allowMultiple: e.target.checked })}>
                      Allow multiple selections
                    </Checkbox>
                  </Form.Item>
                  <Form.Item label={`Options (${q.options.length}/${MAX_OPTIONS})`}>
                    <Space direction="vertical" style={{ display: 'flex' }}>
                      {q.options.map((opt, oIdx) => (
                        <Space key={oIdx} style={{ width: '100%' }}>
                          <Input
                            value={opt.label}
                            maxLength={MAX_OPTION_LABEL}
                            onChange={e => updateOption(q.key, oIdx, e.target.value)}
                            placeholder={`Option ${oIdx + 1}`}
                          />
                          <Button onClick={() => removeOption(q.key, oIdx)} disabled={q.options.length <= MIN_OPTIONS}>
                            ×
                          </Button>
                        </Space>
                      ))}
                      <Button onClick={() => addOption(q.key)} disabled={q.options.length >= MAX_OPTIONS}>
                        Add option
                      </Button>
                    </Space>
                  </Form.Item>
                </>
              )}
            </Form>
          </div>
        ))}
        <Button onClick={addQuestion} disabled={questions.length >= MAX_QUESTIONS}>
          Add question
        </Button>
      </Space>

      <div style={{ marginTop: 24, display: 'flex', gap: 8 }}>
        <Button type="primary" onClick={onSave} loading={saving} disabled={questions.length === 0}>
          {hasSurvey ? 'Save new version' : 'Create survey'}
        </Button>
        <Button onClick={() => navigate('/')} disabled={saving}>
          Cancel
        </Button>
      </div>
    </>
  )
}

type AnswerAggregate = {
  questionId: string
  prompt: string
  type: string
  textCount: number
  ratingCounts: Record<number, number>
  optionCounts: Record<string, number>
  optionLabels: Record<string, string>
}

function buildAggregate(survey: V1Survey | undefined, responses: V1SurveyResponse[]): AnswerAggregate[] {
  const questions = survey?.questions ?? []
  return questions.map(q => {
    const agg: AnswerAggregate = {
      questionId: q.id ?? '',
      prompt: q.prompt ?? '',
      type: typeof q.type === 'string' ? q.type : '',
      textCount: 0,
      ratingCounts: {},
      optionCounts: {},
      optionLabels: Object.fromEntries((q.options ?? []).map(o => [o.id ?? '', o.label ?? '']))
    }
    for (const resp of responses) {
      const answers = (resp.answers ?? []) as V1SurveyAnswer[]
      const a = answers.find(x => x.questionId === q.id)
      if (!a) continue
      if (q.type === QUESTION_TYPE_TEXT && a.text) agg.textCount += 1
      if (q.type === QUESTION_TYPE_RATING && typeof a.rating === 'number') {
        agg.ratingCounts[a.rating] = (agg.ratingCounts[a.rating] ?? 0) + 1
      }
      if (q.type === QUESTION_TYPE_MULTI_CHOICE && a.multiChoice?.optionIds) {
        for (const id of a.multiChoice.optionIds) agg.optionCounts[id] = (agg.optionCounts[id] ?? 0) + 1
      }
    }
    return agg
  })
}

function SurveyResponsesPage() {
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const { playtestId = '' } = useParams<{ playtestId: string }>()

  const playtestQuery = usePlaytesthubServiceAdminApi_GetPlaytest_ByPlaytestId(sdk, { playtestId })
  const playtest = playtestQuery.data?.playtest as V1Playtest | undefined
  const hasSurvey = Boolean(playtest?.surveyId)

  const surveyQuery = usePlaytesthubServiceApi_GetSurveyPlayer_ByPlaytestId(
    sdk,
    { playtestId },
    { enabled: hasSurvey, retry: false }
  )
  const survey = surveyQuery.data?.survey as V1Survey | undefined

  const [surveyIdFilter, setSurveyIdFilter] = useState<string | undefined>(undefined)

  const responsesQuery = usePlaytesthubServiceAdminApi_GetSurveyResponses_ByPlaytestId(sdk, {
    playtestId,
    queryParams: { surveyIdFilter, pageSize: 200 }
  })
  const responses = useMemo(() => (responsesQuery.data?.responses ?? []) as V1SurveyResponse[], [responsesQuery.data])

  const versions = useMemo(() => {
    const seen = new Set<string>()
    for (const r of responses) {
      if (r.surveyId) seen.add(r.surveyId)
    }
    if (survey?.id) seen.add(survey.id)
    return Array.from(seen)
  }, [responses, survey])

  const grouped = useMemo(() => {
    const m = new Map<string, V1SurveyResponse[]>()
    for (const r of responses) {
      const key = r.surveyId ?? 'unknown'
      const arr = m.get(key) ?? []
      arr.push(r)
      m.set(key, arr)
    }
    return m
  }, [responses])

  const aggregate = useMemo(() => buildAggregate(survey, responses), [survey, responses])

  if (playtestQuery.isLoading) return <Spin description="Loading playtest..." />
  if (playtestQuery.error || !playtest) return <Alert type="error" message="Failed to load playtest." />

  const responseColumns = [
    { title: 'Submitted', dataIndex: 'submittedAt', key: 'submittedAt', render: (v: string | null | undefined) => (v ? dayjs(v).format('YYYY-MM-DD HH:mm') : '—') },
    { title: 'User', dataIndex: 'userId', key: 'userId' },
    {
      title: 'Answers',
      key: 'answers',
      render: (_: unknown, row: V1SurveyResponse) => {
        const answers = (row.answers ?? []) as V1SurveyAnswer[]
        return (
          <Space direction="vertical" size={2}>
            {answers.map((a, i) => {
              if (a.text) return <Typography.Text key={i}>“{a.text}”</Typography.Text>
              if (typeof a.rating === 'number') return <Typography.Text key={i}>★ {a.rating}/5</Typography.Text>
              if (a.multiChoice?.optionIds?.length) return <Typography.Text key={i}>✓ {a.multiChoice.optionIds.length} option(s)</Typography.Text>
              return null
            })}
          </Space>
        )
      }
    }
  ]

  return (
    <>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 16 }}>
        <div>
          <Typography.Title level={3} style={{ margin: 0 }}>
            Survey responses — {playtest.title ?? playtest.slug}
          </Typography.Title>
          <Typography.Text type="secondary">{responses.length} response(s) total · {versions.length} version(s)</Typography.Text>
        </div>
        <Space>
          <Select
            allowClear
            placeholder="All versions"
            style={{ minWidth: 240 }}
            value={surveyIdFilter}
            onChange={val => setSurveyIdFilter(val ?? undefined)}
            options={versions.map(v => ({ value: v, label: v === survey?.id ? `${v} (current)` : v }))}
          />
          <Button onClick={() => navigate('/')}>Back</Button>
        </Space>
      </div>

      {!hasSurvey && <Alert type="info" showIcon message="No survey configured for this playtest." />}

      {hasSurvey && (
        <>
          <Typography.Title level={4}>Aggregates</Typography.Title>
          {aggregate.length === 0 && <Typography.Text type="secondary">No survey questions to aggregate.</Typography.Text>}
          <Space direction="vertical" size="middle" style={{ display: 'flex', marginBottom: 24 }}>
            {aggregate.map(a => (
              <div key={a.questionId} data-testid="survey-aggregate" style={{ border: '1px solid #f0f0f0', borderRadius: 6, padding: 12 }}>
                <Typography.Text strong>{a.prompt}</Typography.Text>
                <div style={{ marginTop: 8 }}>
                  {a.type === QUESTION_TYPE_TEXT && <Typography.Text>{a.textCount} text answer(s) — see rows below for content</Typography.Text>}
                  {a.type === QUESTION_TYPE_RATING && (
                    <Space direction="vertical" size={2} style={{ display: 'flex' }}>
                      {[1, 2, 3, 4, 5].map(n => (
                        <div key={n} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                          <span style={{ width: 32 }}>★ {n}</span>
                          <span data-testid={`rating-bar-${a.questionId}-${n}`} style={{ flex: 1 }}>
                            <span
                              style={{
                                display: 'inline-block',
                                height: 8,
                                background: '#1677ff',
                                width: `${Math.min(100, (a.ratingCounts[n] ?? 0) * 20)}%`
                              }}
                            />
                          </span>
                          <span style={{ width: 32, textAlign: 'right' }}>{a.ratingCounts[n] ?? 0}</span>
                        </div>
                      ))}
                    </Space>
                  )}
                  {a.type === QUESTION_TYPE_MULTI_CHOICE && (
                    <Space direction="vertical" size={2} style={{ display: 'flex' }}>
                      {Object.entries(a.optionLabels).map(([id, label]) => (
                        <div key={id} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                          <span style={{ width: 200 }}>{label}</span>
                          <span data-testid={`option-bar-${a.questionId}-${id}`} style={{ flex: 1 }}>
                            <span
                              style={{
                                display: 'inline-block',
                                height: 8,
                                background: '#1677ff',
                                width: `${Math.min(100, (a.optionCounts[id] ?? 0) * 10)}%`
                              }}
                            />
                          </span>
                          <span style={{ width: 32, textAlign: 'right' }}>{a.optionCounts[id] ?? 0}</span>
                        </div>
                      ))}
                    </Space>
                  )}
                </div>
              </div>
            ))}
          </Space>

          <Typography.Title level={4}>Responses</Typography.Title>
          {Array.from(grouped.entries()).map(([surveyVersionId, rows]) => (
            <div key={surveyVersionId} style={{ marginBottom: 24 }}>
              <Typography.Text strong>
                Survey {surveyVersionId === survey?.id ? `${surveyVersionId} (current)` : surveyVersionId}
              </Typography.Text>
              <Table<V1SurveyResponse>
                rowKey={row => row.id ?? ''}
                dataSource={rows}
                columns={responseColumns}
                pagination={{ pageSize: 50 }}
                size="small"
                style={{ marginTop: 8 }}
              />
            </div>
          ))}
        </>
      )}
    </>
  )
}

// Audit-action catalogue mirrors pkg/repo/auditlog_m{1,2,3}.go constants
// (schema.md §"AuditLog — `action` enum"). Used for the action filter dropdown.
const AUDIT_ACTIONS = [
  'nda.accept',
  'nda.edit',
  'playtest.edit',
  'playtest.soft_delete',
  'playtest.status_transition',
  'applicant.approve',
  'applicant.reject',
  'applicant.dm_sent',
  'applicant.dm_failed',
  'dm.circuit_opened',
  'dm.circuit_closed',
  'code.upload',
  'code.upload_rejected',
  'code.grant_orphaned',
  'campaign.create',
  'campaign.create_failed',
  'campaign.generate_codes',
  'campaign.generate_codes_failed',
  'survey.create',
  'survey.edit'
] as const

const ACTOR_SYSTEM = 'system'
const ACTOR_ALL = ''
const ACTOR_USER = '__user__'

type ParsedJson = { ok: true; value: unknown } | { ok: false; raw: string }

function parsePayload(raw: string | null | undefined): ParsedJson {
  if (!raw) return { ok: true, value: null }
  try {
    return { ok: true, value: JSON.parse(raw) }
  } catch {
    return { ok: false, raw }
  }
}

function pretty(v: unknown): string {
  return JSON.stringify(v, null, 2)
}

// Shallow object key diff. Audit payloads in schema.md are flat 1-level
// records, so a shallow comparison covers every documented shape; nested
// values render as embedded JSON within the changed-key row.
type DiffEntry = { key: string; before: unknown; after: unknown; status: 'added' | 'removed' | 'modified' | 'unchanged' }

function shallowDiff(beforeVal: unknown, afterVal: unknown): DiffEntry[] {
  const isObj = (v: unknown): v is Record<string, unknown> =>
    typeof v === 'object' && v !== null && !Array.isArray(v)
  if (!isObj(beforeVal) && !isObj(afterVal)) return []
  const before = isObj(beforeVal) ? beforeVal : {}
  const after = isObj(afterVal) ? afterVal : {}
  const keys = Array.from(new Set([...Object.keys(before), ...Object.keys(after)])).sort()
  return keys.map(key => {
    const inBefore = key in before
    const inAfter = key in after
    if (inBefore && !inAfter) return { key, before: before[key], after: undefined, status: 'removed' }
    if (!inBefore && inAfter) return { key, before: undefined, after: after[key], status: 'added' }
    const same = JSON.stringify(before[key]) === JSON.stringify(after[key])
    return { key, before: before[key], after: after[key], status: same ? 'unchanged' : 'modified' }
  })
}

function AuditDiff({ beforeJson, afterJson }: { beforeJson: string | null | undefined; afterJson: string | null | undefined }) {
  const beforeParsed = parsePayload(beforeJson)
  const afterParsed = parsePayload(afterJson)
  const diff = beforeParsed.ok && afterParsed.ok ? shallowDiff(beforeParsed.value, afterParsed.value) : []
  const changedKeys = diff.filter(d => d.status !== 'unchanged').map(d => d.key)

  return (
    <div data-testid="audit-diff" style={{ background: '#fafafa', padding: 12, borderRadius: 4 }}>
      {changedKeys.length > 0 && (
        <div style={{ marginBottom: 8 }}>
          <Typography.Text strong>Changed keys: </Typography.Text>
          {changedKeys.map(k => (
            <Tag key={k} color="gold" data-testid={`audit-diff-key-${k}`}>
              {k}
            </Tag>
          ))}
        </div>
      )}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
        <div>
          <Typography.Text type="secondary">Before</Typography.Text>
          <pre
            data-testid="audit-before"
            style={{ margin: 0, padding: 8, background: '#fff', border: '1px solid #f0f0f0', borderRadius: 4, maxHeight: 320, overflow: 'auto' }}>
            {beforeParsed.ok ? pretty(beforeParsed.value) : beforeParsed.raw}
          </pre>
        </div>
        <div>
          <Typography.Text type="secondary">After</Typography.Text>
          <pre
            data-testid="audit-after"
            style={{ margin: 0, padding: 8, background: '#fff', border: '1px solid #f0f0f0', borderRadius: 4, maxHeight: 320, overflow: 'auto' }}>
            {afterParsed.ok ? pretty(afterParsed.value) : afterParsed.raw}
          </pre>
        </div>
      </div>
    </div>
  )
}

function AuditLogPage() {
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const { playtestId = '' } = useParams<{ playtestId: string }>()

  const [actorMode, setActorMode] = useState<typeof ACTOR_ALL | typeof ACTOR_SYSTEM | typeof ACTOR_USER>(ACTOR_ALL)
  const [actorUserInput, setActorUserInput] = useState('')
  const [appliedActorUserId, setAppliedActorUserId] = useState('')
  const [actionFilter, setActionFilter] = useState<string | undefined>(undefined)
  const [pageStack, setPageStack] = useState<string[]>([])
  const [currentToken, setCurrentToken] = useState<string>('')

  const actorFilter =
    actorMode === ACTOR_ALL
      ? undefined
      : actorMode === ACTOR_SYSTEM
        ? 'system'
        : appliedActorUserId.trim() || undefined

  const playtestQuery = usePlaytesthubServiceAdminApi_GetPlaytest_ByPlaytestId(sdk, { playtestId })
  const playtest = playtestQuery.data?.playtest as V1Playtest | undefined

  const auditQuery = usePlaytesthubServiceAdminApi_GetAuditLog_ByPlaytestId(sdk, {
    playtestId,
    queryParams: {
      actorFilter,
      actionFilter,
      pageToken: currentToken || undefined,
      pageSize: 50
    }
  })
  const entries = (auditQuery.data?.entries ?? []) as V1AuditLogEntry[]
  const nextPageToken = auditQuery.data?.nextPageToken ?? ''

  const resetPagination = () => {
    setPageStack([])
    setCurrentToken('')
  }

  const onActorModeChange = (val: string) => {
    setActorMode(val as typeof actorMode)
    if (val !== ACTOR_USER) {
      setActorUserInput('')
      setAppliedActorUserId('')
    }
    resetPagination()
  }

  const onActionFilterChange = (val: string | undefined) => {
    setActionFilter(val ?? undefined)
    resetPagination()
  }

  const commitActorUserInput = () => {
    setAppliedActorUserId(actorUserInput)
    resetPagination()
  }

  const onNext = () => {
    if (!nextPageToken) return
    setPageStack(prev => [...prev, currentToken])
    setCurrentToken(nextPageToken)
  }

  const onPrev = () => {
    if (pageStack.length === 0) return
    const prev = [...pageStack]
    const back = prev.pop() ?? ''
    setPageStack(prev)
    setCurrentToken(back)
  }

  const columns = [
    {
      title: 'Time',
      dataIndex: 'createdAt',
      key: 'createdAt',
      width: 180,
      render: (v: string | null | undefined) => (v ? dayjs(v).format('YYYY-MM-DD HH:mm:ss') : '—')
    },
    {
      title: 'Actor',
      dataIndex: 'actorUserId',
      key: 'actorUserId',
      width: 220,
      render: (v: string | null | undefined) =>
        v ? <Typography.Text code>{v}</Typography.Text> : <Tag color="default">system</Tag>
    },
    {
      title: 'Action',
      dataIndex: 'action',
      key: 'action',
      width: 220,
      render: (v: string | null | undefined) => <Tag color="blue">{v ?? '—'}</Tag>
    }
  ]

  if (playtestQuery.isLoading) return <Spin description="Loading playtest..." />
  if (playtestQuery.error || !playtest) return <Alert type="error" message="Failed to load playtest." />

  return (
    <>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 16 }}>
        <div>
          <Typography.Title level={3} style={{ margin: 0 }}>
            Audit log — {playtest.title ?? playtest.slug}
          </Typography.Title>
          <Typography.Text type="secondary">Read-only. System-emitted rows show actor as “system”.</Typography.Text>
        </div>
        <Space>
          <Button onClick={() => auditQuery.refetch()}>Refresh</Button>
          <Button onClick={() => navigate('/')}>Back</Button>
        </Space>
      </div>

      <Space style={{ marginBottom: 16 }} wrap>
        <Select
          aria-label="Actor filter"
          value={actorMode}
          onChange={onActorModeChange}
          style={{ width: 200 }}
          options={[
            { value: ACTOR_ALL, label: 'All actors' },
            { value: ACTOR_SYSTEM, label: 'System' },
            { value: ACTOR_USER, label: 'Admin user…' }
          ]}
        />
        {actorMode === ACTOR_USER && (
          <Input
            aria-label="Actor user id"
            placeholder="Admin user id (UUID), press Enter"
            value={actorUserInput}
            onChange={e => setActorUserInput(e.target.value)}
            onBlur={commitActorUserInput}
            onPressEnter={commitActorUserInput}
            style={{ width: 320 }}
          />
        )}
        <Select
          aria-label="Action filter"
          allowClear
          placeholder="All actions"
          value={actionFilter}
          onChange={onActionFilterChange}
          style={{ width: 280 }}
          options={AUDIT_ACTIONS.map(a => ({ value: a, label: a }))}
        />
      </Space>

      {auditQuery.isLoading && <Spin description="Loading audit log..." />}
      {auditQuery.error && (
        <Alert
          type="error"
          message="Failed to load audit log."
          action={
            <Button size="small" onClick={() => auditQuery.refetch()}>
              Retry
            </Button>
          }
        />
      )}
      {!auditQuery.isLoading && !auditQuery.error && (
        <>
          <Table<V1AuditLogEntry>
            rowKey={row => row.id ?? ''}
            dataSource={entries}
            columns={columns}
            pagination={false}
            expandable={{
              expandedRowRender: row => <AuditDiff beforeJson={row.beforeJson} afterJson={row.afterJson} />,
              rowExpandable: () => true
            }}
          />
          <div style={{ marginTop: 16, display: 'flex', justifyContent: 'space-between' }}>
            <Typography.Text type="secondary">
              {entries.length} row(s){pageStack.length > 0 ? ` · page ${pageStack.length + 1}` : ''}
            </Typography.Text>
            <Space>
              <Button onClick={onPrev} disabled={pageStack.length === 0}>
                Previous
              </Button>
              <Button onClick={onNext} disabled={!nextPageToken}>
                Next
              </Button>
            </Space>
          </div>
        </>
      )}
    </>
  )
}
