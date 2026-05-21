import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import dayjs, { type Dayjs } from 'dayjs'
import utc from 'dayjs/plugin/utc'
import { MemoryRouter } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

dayjs.extend(utc)

vi.mock('@accelbyte/sdk-extend-app-ui', () => ({
  useAppUIContext: () => ({
    sdk: {},
    isCurrentUserHasPermission: () => true
  }),
  CrudType: { READ: 'READ', CREATE: 'CREATE', UPDATE: 'UPDATE', DELETE: 'DELETE' }
}))

const mockGetPlaytests = vi.fn()
const mockGetPlaytest = vi.fn()
const mockCreateMutation = vi.fn()
const mockDeleteMutation = vi.fn()
const mockEditMutation = vi.fn()
const mockTransitionMutation = vi.fn()
const mockGetApplicants = vi.fn()
const mockGetCodes = vi.fn()
const mockApproveMutation = vi.fn()
const mockRejectMutation = vi.fn()
const mockRetryDmMutation = vi.fn()
const mockUploadMutation = vi.fn()
const mockTopUpMutation = vi.fn()
const mockSyncMutation = vi.fn()
const mockGetWorkersHealth = vi.fn()
const mockCompleteAdtLinkMutation = vi.fn()
const mockGetAdtLinkages = vi.fn()
const mockGetAdtBuilds = vi.fn()
const mockStartAdtLinkMutation = vi.fn()
const mockUnlinkAdtMutation = vi.fn()

vi.mock('./playtesthubapi/generated-admin/queries/PlaytesthubServiceAdmin.query', () => ({
  Key_PlaytesthubServiceAdmin: {
    Playtests: 'playtests',
    Playtest: 'playtest',
    Playtest_ByPlaytestId: 'playtest-by-id',
    Playtest_ByPlaytestIdTransitionStatu: 'playtest-by-id-transition',
    Codes_ByPlaytestId: 'codes-by-playtest-id',
    Applicants_ByPlaytestId: 'applicants-by-playtest-id',
    AdtLinkages: 'adt-linkages',
    BuildsAdt_ByAdtLinkageId: 'adt-builds-by-linkage-id'
  },
  usePlaytesthubServiceAdminApi_GetPlaytests: (...args: unknown[]) => mockGetPlaytests(...args),
  usePlaytesthubServiceAdminApi_GetPlaytest_ByPlaytestId: (...args: unknown[]) => mockGetPlaytest(...args),
  usePlaytesthubServiceAdminApi_CreatePlaytestMutation: (...args: unknown[]) => mockCreateMutation(...args),
  usePlaytesthubServiceAdminApi_DeletePlaytest_ByPlaytestIdMutation: (...args: unknown[]) => mockDeleteMutation(...args),
  usePlaytesthubServiceAdminApi_PatchPlaytest_ByPlaytestIdMutation: (...args: unknown[]) => mockEditMutation(...args),
  usePlaytesthubServiceAdminApi_CreatePlaytest_ByPlaytestIdTransitionStatuMutation: (...args: unknown[]) => mockTransitionMutation(...args),
  usePlaytesthubServiceAdminApi_GetApplicants_ByPlaytestId: (...args: unknown[]) => mockGetApplicants(...args),
  usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId: (...args: unknown[]) => mockGetCodes(...args),
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdApproveMutation: (...args: unknown[]) => mockApproveMutation(...args),
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRejectMutation: (...args: unknown[]) => mockRejectMutation(...args),
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRetryDmMutation: (...args: unknown[]) => mockRetryDmMutation(...args),
  usePlaytesthubServiceAdminApi_CreateCodesUpload_ByPlaytestIdMutation: (...args: unknown[]) => mockUploadMutation(...args),
  usePlaytesthubServiceAdminApi_CreateCodesTopUp_ByPlaytestIdMutation: (...args: unknown[]) => mockTopUpMutation(...args),
  usePlaytesthubServiceAdminApi_CreateCodesSyncFromAg_ByPlaytestIdMutation: (...args: unknown[]) => mockSyncMutation(...args),
  usePlaytesthubServiceAdminApi_GetWorkersHealth: (...args: unknown[]) => mockGetWorkersHealth(...args),
  usePlaytesthubServiceAdminApi_CreateAdtLinkagesCompleteMutation: (...args: unknown[]) => mockCompleteAdtLinkMutation(...args),
  usePlaytesthubServiceAdminApi_GetAdtLinkages: (...args: unknown[]) => mockGetAdtLinkages(...args),
  usePlaytesthubServiceAdminApi_GetBuildsAdt_ByAdtLinkageId: (...args: unknown[]) => mockGetAdtBuilds(...args),
  usePlaytesthubServiceAdminApi_CreateAdtLinkagesStartMutation: (...args: unknown[]) => mockStartAdtLinkMutation(...args),
  usePlaytesthubServiceAdminApi_DeleteAdtLinkage_ByAdtLinkageIdMutation: (...args: unknown[]) => mockUnlinkAdtMutation(...args)
}))

import { FederatedElement } from './federated-element'
import { dateRangeWindowRule } from './window'

function renderAt(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <FederatedElement />
      </MemoryRouter>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  mockGetPlaytests.mockReset()
  mockGetPlaytest.mockReset()
  mockCreateMutation.mockReset()
  mockDeleteMutation.mockReset()
  mockEditMutation.mockReset()
  mockTransitionMutation.mockReset()
  mockGetApplicants.mockReset()
  mockGetCodes.mockReset()
  mockApproveMutation.mockReset()
  mockRejectMutation.mockReset()
  mockRetryDmMutation.mockReset()
  mockUploadMutation.mockReset()
  mockTopUpMutation.mockReset()
  mockSyncMutation.mockReset()
  mockGetWorkersHealth.mockReset()
  mockCompleteAdtLinkMutation.mockReset()
  mockGetAdtLinkages.mockReset()
  mockGetAdtBuilds.mockReset()
  mockStartAdtLinkMutation.mockReset()
  mockUnlinkAdtMutation.mockReset()

  // Default: empty list + no-op mutations.
  mockGetPlaytests.mockReturnValue({ data: { playtests: [] }, isLoading: false, error: null, refetch: vi.fn() })
  mockCreateMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockDeleteMutation.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockEditMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockTransitionMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockGetPlaytest.mockReturnValue({ data: undefined, isLoading: false, error: null })
  mockGetApplicants.mockReturnValue({ data: { applicants: [] }, isLoading: false, error: null, refetch: vi.fn() })
  mockGetCodes.mockReturnValue({ data: { stats: { total: 0, unused: 0, reserved: 0, granted: 0 }, codes: [] }, isLoading: false, error: null, refetch: vi.fn() })
  mockApproveMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockRejectMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockRetryDmMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockUploadMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockTopUpMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockSyncMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockGetWorkersHealth.mockReturnValue({ data: { workers: [] }, isLoading: false, error: null })
  mockCompleteAdtLinkMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockGetAdtLinkages.mockReturnValue({ data: { linkages: [] }, isLoading: false, error: null })
  mockGetAdtBuilds.mockReturnValue({ data: { builds: [] }, isLoading: false, error: null })
  mockStartAdtLinkMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockUnlinkAdtMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
})

describe('PlaytestsListPage', () => {
  it('renders empty state heading and a new-playtest button', () => {
    renderAt('/')
    expect(screen.getByRole('heading', { name: /playtests/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /new playtest/i })).toBeInTheDocument()
  })

  it('renders each returned playtest in the table', () => {
    mockGetPlaytests.mockReturnValue({
      data: {
        playtests: [
          {
            id: 'pt_1',
            slug: 'summer-alpha',
            title: 'Summer Alpha',
            status: 'PLAYTEST_STATUS_OPEN',
            distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
            createdAt: '2026-04-19T00:00:00Z'
          }
        ]
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })

    renderAt('/')
    expect(screen.getByText('summer-alpha')).toBeInTheDocument()
    expect(screen.getByText('Summer Alpha')).toBeInTheDocument()
    expect(screen.getByText('Open')).toBeInTheDocument()
    expect(screen.getByText('Steam keys')).toBeInTheDocument()
  })

  it('shows a Publish button on DRAFT rows that transitions to OPEN', async () => {
    const mutate = vi.fn()
    mockTransitionMutation.mockReturnValue({ mutate, isPending: false, isError: false, error: null })
    mockGetPlaytests.mockReturnValue({
      data: { playtests: [{ id: 'pt_1', slug: 'summer-alpha', title: 'Summer Alpha', status: 'PLAYTEST_STATUS_DRAFT' }] },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })

    renderAt('/')
    const user = userEvent.setup()
    const publishBtn = screen.getByRole('button', { name: /^publish$/i })
    await user.click(publishBtn)
    const popup = await screen.findByRole('tooltip')
    await user.click(within(popup).getByRole('button', { name: /^publish$/i }))
    await waitFor(() =>
      expect(mutate).toHaveBeenCalledWith({ playtestId: 'pt_1', data: { targetStatus: 'PLAYTEST_STATUS_OPEN' } })
    )
  })

  it('shows a Close button on OPEN rows that transitions to CLOSED', async () => {
    const mutate = vi.fn()
    mockTransitionMutation.mockReturnValue({ mutate, isPending: false, isError: false, error: null })
    mockGetPlaytests.mockReturnValue({
      data: { playtests: [{ id: 'pt_1', slug: 'summer-alpha', title: 'Summer Alpha', status: 'PLAYTEST_STATUS_OPEN' }] },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })

    renderAt('/')
    const user = userEvent.setup()
    const closeBtn = screen.getByRole('button', { name: /^close$/i })
    await user.click(closeBtn)
    const popup = await screen.findByRole('tooltip')
    await user.click(within(popup).getByRole('button', { name: /^close$/i }))
    await waitFor(() =>
      expect(mutate).toHaveBeenCalledWith({ playtestId: 'pt_1', data: { targetStatus: 'PLAYTEST_STATUS_CLOSED' } })
    )
  })

  it('does not show a transition button on CLOSED rows', () => {
    mockGetPlaytests.mockReturnValue({
      data: { playtests: [{ id: 'pt_1', slug: 'summer-alpha', title: 'Summer Alpha', status: 'PLAYTEST_STATUS_CLOSED' }] },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderAt('/')
    expect(screen.queryByRole('button', { name: /^publish$/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^close$/i })).not.toBeInTheDocument()
  })

  it('calls DeletePlaytest mutation when soft-delete is confirmed', async () => {
    const mutate = vi.fn()
    mockDeleteMutation.mockReturnValue({ mutate, isPending: false })
    mockGetPlaytests.mockReturnValue({
      data: { playtests: [{ id: 'pt_1', slug: 'summer-alpha', title: 'Summer Alpha', status: 'PLAYTEST_STATUS_DRAFT' }] },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })

    renderAt('/')
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /^delete$/i }))
    // Popconfirm renders "Delete" in the confirm popup as well — pick the danger one inside the popup.
    const popup = await screen.findByRole('tooltip')
    await user.click(within(popup).getByRole('button', { name: /^delete$/i }))
    await waitFor(() => expect(mutate).toHaveBeenCalledWith({ playtestId: 'pt_1' }))
  })
})

describe('PlaytestCreatePage', () => {
  it('offers both distribution models with STEAM_KEYS as default', () => {
    renderAt('/new')
    const agsRadio = screen.getByRole('radio', { name: /AGS Campaign/i })
    expect(agsRadio).toBeEnabled()
    const steamRadio = screen.getByRole('radio', { name: /Steam keys/i })
    expect(steamRadio).toBeEnabled()
    expect(steamRadio).toBeChecked()
  })

  it('shows all the PRD-required fields on the create form', () => {
    renderAt('/new')
    expect(screen.getByLabelText(/slug/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/^title$/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/banner image url/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/platforms/i)).toBeInTheDocument()
    expect(screen.getByText(/distribution model/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^create$/i })).toBeInTheDocument()
  })

  it('auto-approve toggle starts off and hides the limit input', () => {
    renderAt('/new')
    const toggle = screen.getByRole('switch', { name: /auto-approve/i })
    expect(toggle).not.toBeChecked()
    expect(screen.queryByLabelText(/auto-approve limit/i)).not.toBeInTheDocument()
  })

  it('reveals the auto-approve limit input when the toggle is on', async () => {
    renderAt('/new')
    const user = userEvent.setup()
    await user.click(screen.getByRole('switch', { name: /auto-approve/i }))
    expect(await screen.findByLabelText(/auto-approve limit/i)).toBeInTheDocument()
  })

  it('rejects an out-of-bounds auto-approve limit with the byte-exact server message', async () => {
    const mutate = vi.fn()
    mockCreateMutation.mockReturnValue({ mutate, isPending: false, isError: false, error: null })
    renderAt('/new')
    const user = userEvent.setup()
    await user.click(screen.getByRole('switch', { name: /auto-approve/i }))
    const limit = await screen.findByLabelText(/auto-approve limit/i)
    await user.type(limit, '100001')
    // Required fields so the form actually reaches the validator.
    await user.type(screen.getByLabelText(/slug/i), 'demo-slug')
    await user.type(screen.getByLabelText(/^title$/i), 'Demo')
    await user.click(screen.getByRole('button', { name: /^create$/i }))
    expect(
      await screen.findByText('auto_approve_limit must be between 1 and 100000 when auto_approve is true')
    ).toBeInTheDocument()
    expect(mutate).not.toHaveBeenCalled()
  })

  it('offers the ADT distribution radio (M5.B)', () => {
    renderAt('/new')
    expect(screen.getByRole('radio', { name: /^ADT$/i })).toBeEnabled()
  })

  it('reveals the ADT field set when ADT is picked', async () => {
    mockGetAdtLinkages.mockReturnValue({
      data: { linkages: [{ id: 'lnk-1', adtNamespace: 'adt-ns-1', studioNamespace: 'studio-A' }] },
      isLoading: false,
      error: null
    })
    renderAt('/new')
    const user = userEvent.setup()
    await user.click(screen.getByRole('radio', { name: /^ADT$/i }))
    expect(await screen.findByLabelText(/adt linkage/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/adt game id/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/adt build id/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/static fallback download url/i)).toBeInTheDocument()
  })
})

describe('ADTLinkagesPanel', () => {
  it('renders empty-state copy when no linkages exist', () => {
    renderAt('/')
    expect(screen.getByText(/no ADT linkages yet/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /link new ADT namespace/i })).toBeInTheDocument()
  })

  it('renders linkage rows + an Unlink button per row', () => {
    mockGetAdtLinkages.mockReturnValue({
      data: { linkages: [{ id: 'lnk-1', adtNamespace: 'adt-ns-1', studioNamespace: 'studio-A', linkedAt: '2026-05-19T00:00:00Z' }] },
      isLoading: false,
      error: null
    })
    renderAt('/')
    expect(screen.getByText('adt-ns-1')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /unlink/i })).toBeInTheDocument()
  })

  it('opens the Link ADT modal on click', async () => {
    renderAt('/')
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /link new ADT namespace/i }))
    expect(await screen.findByText(/you will be redirected to ADT to authorise the linkage/i)).toBeInTheDocument()
  })
})

describe('PlaytestEditPage', () => {
  it('renders a loading spinner while fetching', () => {
    mockGetPlaytest.mockReturnValue({ data: undefined, isLoading: true, error: null })
    renderAt('/pt_1/edit')
    expect(screen.getByText(/loading playtest/i)).toBeInTheDocument()
  })

  it('pre-fills the form with the fetched playtest', async () => {
    mockGetPlaytest.mockReturnValue({
      data: {
        playtest: {
          id: 'pt_1',
          slug: 'summer-alpha',
          title: 'Summer Alpha',
          description: 'Alpha description',
          platforms: ['PLATFORM_STEAM'],
          distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
          ndaRequired: false
        }
      },
      isLoading: false,
      error: null
    })
    renderAt('/pt_1/edit')
    await waitFor(() => expect(screen.getByDisplayValue('Summer Alpha')).toBeInTheDocument())
    expect(screen.getByDisplayValue('Alpha description')).toBeInTheDocument()
    expect(screen.getByText(/immutable after creation/i)).toBeInTheDocument()
  })

  it('preloads auto-approve toggle + limit from the fetched playtest', async () => {
    mockGetPlaytest.mockReturnValue({
      data: {
        playtest: {
          id: 'pt_1',
          slug: 'summer-alpha',
          title: 'Summer Alpha',
          platforms: ['PLATFORM_STEAM'],
          distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
          ndaRequired: false,
          autoApprove: true,
          autoApproveLimit: 25
        }
      },
      isLoading: false,
      error: null
    })
    renderAt('/pt_1/edit')
    await waitFor(() => expect(screen.getByDisplayValue('Summer Alpha')).toBeInTheDocument())
    expect(screen.getByRole('switch', { name: /auto-approve/i })).toBeChecked()
    expect(screen.getByLabelText(/auto-approve limit/i)).toHaveValue('25')
  })
})
describe('Playtest window (M4)', () => {
  it('labels create-form date range as UTC and surfaces auto-transition help', () => {
    renderAt('/new')
    expect(screen.getByText('Starts / Ends (UTC)')).toBeInTheDocument()
    expect(screen.queryByText(/display-only in MVP/i)).not.toBeInTheDocument()
    expect(screen.getByText(/Auto-opens at start, auto-closes at end/i)).toBeInTheDocument()
  })

  it('labels edit-form date range as UTC', async () => {
    mockGetPlaytest.mockReturnValue({
      data: {
        playtest: {
          id: 'pt_1',
          slug: 'summer-alpha',
          title: 'Summer Alpha',
          platforms: ['PLATFORM_STEAM'],
          distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
          ndaRequired: false,
          startsAt: '2026-06-01T10:00:00Z',
          endsAt: '2026-06-08T10:00:00Z'
        }
      },
      isLoading: false,
      error: null
    })
    renderAt('/pt_1/edit')
    await waitFor(() => expect(screen.getByDisplayValue('Summer Alpha')).toBeInTheDocument())
    expect(screen.getByText('Starts / Ends (UTC)')).toBeInTheDocument()
  })

  it('validator rejects inverted + equal windows with byte-exact server message and accepts a valid window', async () => {
    // Driving the antd RangePicker through userEvent in jsdom is fragile; the byte-exact server
    // error string is owned by the validator rule, so we exercise it directly.
    const inverted: [Dayjs, Dayjs] = [dayjs.utc('2026-06-08T10:00:00Z'), dayjs.utc('2026-06-01T10:00:00Z')]
    await expect(dateRangeWindowRule.validator(undefined, inverted)).rejects.toThrow('ends_at must be after starts_at')
    const equal: [Dayjs, Dayjs] = [dayjs.utc('2026-06-08T10:00:00Z'), dayjs.utc('2026-06-08T10:00:00Z')]
    await expect(dateRangeWindowRule.validator(undefined, equal)).rejects.toThrow('ends_at must be after starts_at')
    const valid: [Dayjs, Dayjs] = [dayjs.utc('2026-06-01T10:00:00Z'), dayjs.utc('2026-06-08T10:00:00Z')]
    await expect(dateRangeWindowRule.validator(undefined, valid)).resolves.toBeUndefined()
    const onlyStart: [Dayjs, null] = [dayjs.utc('2026-06-01T10:00:00Z'), null]
    await expect(dateRangeWindowRule.validator(undefined, onlyStart)).resolves.toBeUndefined()
    const onlyEnd: [null, Dayjs] = [null, dayjs.utc('2026-06-08T10:00:00Z')]
    await expect(dateRangeWindowRule.validator(undefined, onlyEnd)).resolves.toBeUndefined()
  })

  it('renders an Auto-opens tooltip on DRAFT rows with startsAt set', async () => {
    mockGetPlaytests.mockReturnValue({
      data: {
        playtests: [
          {
            id: 'pt_1',
            slug: 'summer-alpha',
            title: 'Summer Alpha',
            status: 'PLAYTEST_STATUS_DRAFT',
            startsAt: dayjs.utc().add(2, 'hour').toISOString()
          }
        ]
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderAt('/')
    const user = userEvent.setup()
    await user.hover(screen.getByText('Draft'))
    const tip = await screen.findByRole('tooltip')
    expect(tip.textContent).toMatch(/Auto-opens/)
  })

  it('renders an Auto-closes tooltip on OPEN rows with endsAt set', async () => {
    mockGetPlaytests.mockReturnValue({
      data: {
        playtests: [
          {
            id: 'pt_1',
            slug: 'summer-alpha',
            title: 'Summer Alpha',
            status: 'PLAYTEST_STATUS_OPEN',
            endsAt: dayjs.utc().add(3, 'day').toISOString()
          }
        ]
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderAt('/')
    const user = userEvent.setup()
    await user.hover(screen.getByText('Open'))
    const tip = await screen.findByRole('tooltip')
    expect(tip.textContent).toMatch(/Auto-closes/)
  })

  it('shows a red Alert banner when any worker is stale', () => {
    mockGetWorkersHealth.mockReturnValue({
      data: {
        workers: [
          { name: 'reclaim-job', stale: false },
          { name: 'window-worker', stale: true }
        ]
      },
      isLoading: false,
      error: null
    })
    renderAt('/')
    const banner = screen.getByTestId('worker-health-banner')
    expect(banner).toBeInTheDocument()
    expect(banner.textContent).toMatch(/window-worker/)
    expect(banner.textContent).toMatch(/Auto-transitions are paused/)
  })

  it('does not render the banner when every worker is fresh', () => {
    mockGetWorkersHealth.mockReturnValue({
      data: { workers: [{ name: 'reclaim-job', stale: false }, { name: 'window-worker', stale: false }] },
      isLoading: false,
      error: null
    })
    renderAt('/')
    expect(screen.queryByTestId('worker-health-banner')).not.toBeInTheDocument()
  })

  it('does not render a tooltip on DRAFT rows without startsAt', () => {
    mockGetPlaytests.mockReturnValue({
      data: {
        playtests: [{ id: 'pt_1', slug: 'manual', title: 'Manual', status: 'PLAYTEST_STATUS_DRAFT' }]
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderAt('/')
    // No hover; tooltip should not have a trigger wrapper. Tag rendered raw.
    expect(screen.getByText('Draft')).toBeInTheDocument()
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
  })
})

describe('ADTLinkCallbackPage', () => {
  it('calls CompleteADTLink with state + adt_namespace from the query string', async () => {
    const mutate = vi.fn()
    mockCompleteAdtLinkMutation.mockReturnValue({ mutate, isPending: false, isError: false, error: null })
    renderAt('/adt-link-callback?state=abc123&result=success&adt_namespace=adt-studio-1')
    await waitFor(() => expect(mutate).toHaveBeenCalledTimes(1))
    expect(mutate).toHaveBeenCalledWith({ data: { state: 'abc123', adtNamespace: 'adt-studio-1' } })
  })

  it('surfaces an error and does not invoke the mutation when state is missing', async () => {
    const mutate = vi.fn()
    mockCompleteAdtLinkMutation.mockReturnValue({ mutate, isPending: false, isError: false, error: null })
    renderAt('/adt-link-callback?result=success&adt_namespace=adt-studio-1')
    expect(await screen.findByText(/missing the state or adt_namespace/i)).toBeInTheDocument()
    expect(mutate).not.toHaveBeenCalled()
  })

  it('surfaces an error when ADT reports the link as failed', async () => {
    const mutate = vi.fn()
    mockCompleteAdtLinkMutation.mockReturnValue({ mutate, isPending: false, isError: false, error: null })
    renderAt('/adt-link-callback?state=abc&adt_namespace=adt-studio-1&result=failed&reason=user_declined')
    expect(await screen.findByText(/user_declined/)).toBeInTheDocument()
    expect(mutate).not.toHaveBeenCalled()
  })
})
