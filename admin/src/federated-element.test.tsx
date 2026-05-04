import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

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

vi.mock('./playtesthubapi/generated-admin/queries/PlaytesthubServiceAdmin.query', () => ({
  Key_PlaytesthubServiceAdmin: {
    Playtests: 'playtests',
    Playtest: 'playtest',
    Playtest_ByPlaytestId: 'playtest-by-id',
    Playtest_ByPlaytestIdTransitionStatu: 'playtest-by-id-transition',
    Codes_ByPlaytestId: 'codes-by-playtest-id',
    Applicants_ByPlaytestId: 'applicants-by-playtest-id'
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
  usePlaytesthubServiceAdminApi_CreateCodesSyncFromAg_ByPlaytestIdMutation: (...args: unknown[]) => mockSyncMutation(...args)
}))

import { FederatedElement } from './federated-element'

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
})

describe('ApplicantsPage', () => {
  it('renders applicant rows with status tag', () => {
    mockGetPlaytest.mockReturnValue({
      data: { playtest: { id: 'pt_1', slug: 'summer-alpha', title: 'Summer Alpha' } },
      isLoading: false,
      error: null
    })
    mockGetApplicants.mockReturnValue({
      data: {
        applicants: [
          {
            id: 'app_1',
            discordHandle: 'tester#0001',
            platforms: ['PLATFORM_STEAM'],
            status: 'APPLICANT_STATUS_PENDING',
            createdAt: '2026-05-01T00:00:00Z'
          }
        ]
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderAt('/pt_1/applicants')
    expect(screen.getByText('tester#0001')).toBeInTheDocument()
    expect(screen.getByText('Pending')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^approve$/i })).toBeEnabled()
    expect(screen.getByRole('button', { name: /^reject$/i })).toBeEnabled()
  })

  it('triggers approve mutation on confirm', async () => {
    const mutate = vi.fn()
    mockApproveMutation.mockReturnValue({ mutate, isPending: false, isError: false, error: null })
    mockGetApplicants.mockReturnValue({
      data: {
        applicants: [{ id: 'app_1', discordHandle: 'tester', status: 'APPLICANT_STATUS_PENDING' }]
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderAt('/pt_1/applicants')
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /^approve$/i }))
    const popup = await screen.findByRole('tooltip')
    await user.click(within(popup).getByRole('button', { name: /^approve$/i }))
    await waitFor(() => expect(mutate).toHaveBeenCalledWith({ applicantId: 'app_1', data: {} }))
  })

  it('shows Retry DM only for APPROVED applicants whose last DM failed', () => {
    mockGetApplicants.mockReturnValue({
      data: {
        applicants: [
          { id: 'app_1', discordHandle: 'a', status: 'APPLICANT_STATUS_APPROVED', lastDmStatus: 'DM_STATUS_FAILED' },
          { id: 'app_2', discordHandle: 'b', status: 'APPLICANT_STATUS_APPROVED', lastDmStatus: 'DM_STATUS_SENT' },
          { id: 'app_3', discordHandle: 'c', status: 'APPLICANT_STATUS_PENDING' }
        ]
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderAt('/pt_1/applicants')
    const retryBtns = screen.getAllByRole('button', { name: /retry dm/i })
    expect(retryBtns).toHaveLength(1)
  })

  it('renders the low-pool banner when unused/total ≤ 10%', () => {
    mockGetCodes.mockReturnValue({
      data: { stats: { total: 100, unused: 5, reserved: 0, granted: 95 }, codes: [] },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderAt('/pt_1/applicants')
    expect(screen.getByText(/code pool is low/i)).toBeInTheDocument()
  })

  it('opens the reject modal and submits the typed reason', async () => {
    const mutate = vi.fn()
    mockRejectMutation.mockReturnValue({ mutate, isPending: false, isError: false, error: null })
    mockGetApplicants.mockReturnValue({
      data: { applicants: [{ id: 'app_1', discordHandle: 'tester', status: 'APPLICANT_STATUS_PENDING' }] },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderAt('/pt_1/applicants')
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /^reject$/i }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByPlaceholderText(/stored on the applicant row/i), 'duplicate signup')
    await user.click(within(dialog).getByRole('button', { name: /^reject$/i }))
    await waitFor(() => expect(mutate).toHaveBeenCalledWith({ applicantId: 'app_1', data: { rejectionReason: 'duplicate signup' } }))
  })
})

describe('CodePoolPage', () => {
  it('renders pool stats and the upload section for STEAM_KEYS playtests', () => {
    mockGetPlaytest.mockReturnValue({
      data: {
        playtest: {
          id: 'pt_1',
          slug: 'summer-alpha',
          title: 'Summer Alpha',
          distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS'
        }
      },
      isLoading: false,
      error: null
    })
    mockGetCodes.mockReturnValue({
      data: { stats: { total: 100, unused: 80, reserved: 5, granted: 15 }, codes: [] },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderAt('/pt_1/codes')
    expect(screen.getByRole('heading', { name: /upload steam keys/i })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: /generate \/ sync/i })).not.toBeInTheDocument()
    expect(screen.getByText('80')).toBeInTheDocument()
  })

  it('renders generate/sync controls for AGS_CAMPAIGN playtests', () => {
    mockGetPlaytest.mockReturnValue({
      data: {
        playtest: {
          id: 'pt_1',
          slug: 'summer-alpha',
          title: 'Summer Alpha',
          distributionModel: 'DISTRIBUTION_MODEL_AGS_CAMPAIGN'
        }
      },
      isLoading: false,
      error: null
    })
    renderAt('/pt_1/codes')
    expect(screen.getByRole('heading', { name: /generate \/ sync ags campaign codes/i })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: /upload steam keys/i })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /generate more codes/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /sync from ags/i })).toBeInTheDocument()
  })

  it('triggers top-up mutation with the entered quantity', async () => {
    const mutate = vi.fn()
    mockTopUpMutation.mockReturnValue({ mutate, isPending: false, isError: false, error: null })
    mockGetPlaytest.mockReturnValue({
      data: { playtest: { id: 'pt_1', slug: 'a', title: 'A', distributionModel: 'DISTRIBUTION_MODEL_AGS_CAMPAIGN' } },
      isLoading: false,
      error: null
    })
    renderAt('/pt_1/codes')
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /generate more codes/i }))
    await waitFor(() => expect(mutate).toHaveBeenCalledWith({ playtestId: 'pt_1', data: { quantity: 100 } }))
  })

  it('triggers sync mutation', async () => {
    const mutate = vi.fn()
    mockSyncMutation.mockReturnValue({ mutate, isPending: false, isError: false, error: null })
    mockGetPlaytest.mockReturnValue({
      data: { playtest: { id: 'pt_1', slug: 'a', title: 'A', distributionModel: 'DISTRIBUTION_MODEL_AGS_CAMPAIGN' } },
      isLoading: false,
      error: null
    })
    renderAt('/pt_1/codes')
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /sync from ags/i }))
    await waitFor(() => expect(mutate).toHaveBeenCalledWith({ playtestId: 'pt_1', data: {} }))
  })
})
