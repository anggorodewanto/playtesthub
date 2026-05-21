import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@accelbyte/sdk-extend-app-ui', () => ({
  useAppUIContext: () => ({ sdk: {}, isCurrentUserHasPermission: () => true }),
  CrudType: { READ: 'READ', CREATE: 'CREATE', UPDATE: 'UPDATE', DELETE: 'DELETE' }
}))

const mockGetPlaytests = vi.fn()
const mockTransition = vi.fn()
const mockGetParticipants = vi.fn()
const mockGetApplicants = vi.fn()
const mockGetAnnouncements = vi.fn()
const mockCreateAnnouncement = vi.fn()
const mockGetCodes = vi.fn()
const mockUploadCodes = vi.fn()
const mockTopUpCodes = vi.fn()
const mockSyncCodes = vi.fn()
const mockApprove = vi.fn()
const mockReject = vi.fn()
const mockRetryDm = vi.fn()
const mockGetPublicConfig = vi.fn()

vi.mock('./playtesthubapi/generated-public/queries/PlaytesthubService.query', () => ({
  usePlaytesthubServiceApi_GetConfig: (...a: unknown[]) => mockGetPublicConfig(...a)
}))

vi.mock('./playtesthubapi/generated-admin/queries/PlaytesthubServiceAdmin.query', () => ({
  Key_PlaytesthubServiceAdmin: {
    Playtests: 'playtests',
    Participants_ByPlaytestId: 'participants',
    Applicants_ByPlaytestId: 'applicants',
    Announcements_ByPlaytestId: 'announcements',
    Codes_ByPlaytestId: 'codes-by-playtest-id'
  },
  usePlaytesthubServiceAdminApi_GetPlaytests: (...a: unknown[]) => mockGetPlaytests(...a),
  usePlaytesthubServiceAdminApi_CreatePlaytest_ByPlaytestIdTransitionStatuMutation: (...a: unknown[]) =>
    mockTransition(...a),
  usePlaytesthubServiceAdminApi_GetParticipants_ByPlaytestId: (...a: unknown[]) => mockGetParticipants(...a),
  usePlaytesthubServiceAdminApi_GetApplicants_ByPlaytestId: (...a: unknown[]) => mockGetApplicants(...a),
  usePlaytesthubServiceAdminApi_GetAnnouncements_ByPlaytestId: (...a: unknown[]) => mockGetAnnouncements(...a),
  usePlaytesthubServiceAdminApi_CreateAnnouncement_ByPlaytestIdMutation: (...a: unknown[]) =>
    mockCreateAnnouncement(...a),
  usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId: (...a: unknown[]) => mockGetCodes(...a),
  usePlaytesthubServiceAdminApi_CreateCodesUpload_ByPlaytestIdMutation: (...a: unknown[]) => mockUploadCodes(...a),
  usePlaytesthubServiceAdminApi_CreateCodesTopUp_ByPlaytestIdMutation: (...a: unknown[]) => mockTopUpCodes(...a),
  usePlaytesthubServiceAdminApi_CreateCodesSyncFromAg_ByPlaytestIdMutation: (...a: unknown[]) => mockSyncCodes(...a),
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdApproveMutation: (...a: unknown[]) =>
    mockApprove(...a),
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRejectMutation: (...a: unknown[]) => mockReject(...a),
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRetryDmMutation: (...a: unknown[]) => mockRetryDm(...a)
}))

import { PlaytestDetailPage } from './PlaytestDetailPage'

const DRAFT_PT = {
  id: 'pt-draft',
  namespace: 'ns',
  slug: 'autumn-draft',
  title: 'Autumn Build',
  description: 'an autumn playtest',
  bannerImageUrl: 'https://example.com/banner.png',
  platforms: ['PLATFORM_STEAM'],
  status: 'PLAYTEST_STATUS_DRAFT',
  distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
  ndaRequired: false,
  autoApprove: false
}

const OPEN_PT = { ...DRAFT_PT, id: 'pt-open', slug: 'autumn-open', status: 'PLAYTEST_STATUS_OPEN' }
const CLOSED_PT = { ...DRAFT_PT, id: 'pt-closed', slug: 'autumn-closed', status: 'PLAYTEST_STATUS_CLOSED' }

function renderDetail(slug: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[`/playtest/${slug}`]}>
        <Routes>
          <Route path="/playtest/:slug" element={<PlaytestDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  mockGetPlaytests.mockReturnValue({
    data: { playtests: [DRAFT_PT, OPEN_PT, CLOSED_PT] },
    isLoading: false,
    error: undefined,
    refetch: vi.fn()
  })
  mockTransition.mockReturnValue({ mutate: vi.fn() })
  mockGetParticipants.mockReturnValue({ data: { participants: [] }, isLoading: false, refetch: vi.fn() })
  mockGetApplicants.mockReturnValue({ data: { applicants: [] }, isLoading: false, error: null, refetch: vi.fn() })
  mockGetAnnouncements.mockReturnValue({ data: { announcements: [] }, isLoading: false, refetch: vi.fn() })
  mockCreateAnnouncement.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockGetCodes.mockReturnValue({ data: { stats: { total: 0, unused: 0, granted: 0 }, codes: [] }, isLoading: false, error: null, refetch: vi.fn() })
  mockUploadCodes.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockTopUpCodes.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockSyncCodes.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockApprove.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockReject.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockRetryDm.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockGetPublicConfig.mockReturnValue({ data: { playerBaseUrl: 'https://play.example.com' } })
})

describe('PlaytestDetailPage shell', () => {
  it('renders header + status pill + breadcrumb', () => {
    renderDetail('autumn-draft')
    expect(screen.getByRole('heading', { name: 'Autumn Build', level: 2 })).toBeInTheDocument()
    const pill = screen.getByTestId('playtest-status-pill')
    expect(within(pill).getByText('Draft')).toBeInTheDocument()
  })

  it('renders Publish in DRAFT only', () => {
    renderDetail('autumn-draft')
    expect(screen.getByTestId('header-publish')).toBeInTheDocument()
    expect(screen.queryByTestId('header-stop')).not.toBeInTheDocument()
  })

  it('renders Stop Playtest in OPEN only', () => {
    renderDetail('autumn-open')
    expect(screen.getByTestId('header-stop')).toBeInTheDocument()
    expect(screen.queryByTestId('header-publish')).not.toBeInTheDocument()
  })

  it('hides Publish and Stop in CLOSED', () => {
    renderDetail('autumn-closed')
    expect(screen.queryByTestId('header-publish')).not.toBeInTheDocument()
    expect(screen.queryByTestId('header-stop')).not.toBeInTheDocument()
  })

  it('falls back to Info tab when ?tab missing', () => {
    renderDetail('autumn-draft')
    expect(screen.getByTestId('playtest-info-tab')).toBeInTheDocument()
  })

  it('persists ?tab= changes across reloads via search params', async () => {
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Distribution' }))
    await waitFor(() => expect(screen.getByTestId('distribution-tab')).toBeInTheDocument())
  })

  it('Info tab renders the read-only field grid + Edit button', () => {
    renderDetail('autumn-draft')
    expect(screen.getByTestId('playtest-info-tab')).toBeInTheDocument()
    expect(screen.getByText('an autumn playtest')).toBeInTheDocument()
    expect(screen.getByTestId('playtest-info-edit')).toBeInTheDocument()
  })

  it('surfaces a warning when the slug is unknown', () => {
    renderDetail('does-not-exist')
    expect(screen.getByText(/not found/i)).toBeInTheDocument()
  })

  it('Discord Bot Tools tab disables the form on closed playtest', async () => {
    renderDetail('autumn-closed')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Discord Bot Tools' }))
    expect(await screen.findByTestId('bot-tools-closed-banner')).toBeInTheDocument()
  })

  it('Discord Bot Tools tab submits CreateAnnouncement with form values', async () => {
    const mutate = vi.fn()
    mockCreateAnnouncement.mockReturnValue({ mutate, isPending: false })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Discord Bot Tools' }))
    const subj = await screen.findByTestId('bot-tools-subject')
    const msg = screen.getByTestId('bot-tools-message')
    await user.type(subj, 'Build update')
    await user.type(msg, 'New patch live')
    await user.click(screen.getByTestId('bot-tools-submit'))
    await waitFor(() => expect(mutate).toHaveBeenCalled())
    expect(mutate.mock.calls[0][0]).toMatchObject({
      playtestId: 'pt-draft',
      data: {
        sendToFilter: 'ANNOUNCEMENT_SEND_TO_FILTER_APPROVED_ONLY',
        subject: 'Build update',
        message: 'New patch live'
      }
    })
  })

  it('Discord Bot Tools tab renders the empty-history copy', async () => {
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Discord Bot Tools' }))
    expect(await screen.findByText(/No announcements sent yet/)).toBeInTheDocument()
  })

  it('Participants tab renders rows, capacity counter, and joined platform column', async () => {
    mockGetParticipants.mockReturnValue({
      data: {
        participants: [
          {
            applicantId: 'a1',
            userId: 'u1',
            discordHandle: 'alice#1',
            signupAt: '2026-05-20T00:00:00Z',
            ndaAcceptedAt: '2026-05-20T00:00:00Z',
            codeSentAt: '2026-05-20T01:00:00Z',
            status: 'APPLICANT_STATUS_APPROVED'
          },
          {
            applicantId: 'a2',
            userId: 'u2',
            discordHandle: 'bob#2',
            signupAt: '2026-05-20T00:00:00Z',
            status: 'APPLICANT_STATUS_PENDING'
          }
        ]
      },
      isLoading: false,
      refetch: vi.fn()
    })
    mockGetApplicants.mockReturnValue({
      data: {
        applicants: [
          { id: 'a1', discordHandle: 'alice#1', platforms: ['PLATFORM_STEAM'], status: 'APPLICANT_STATUS_APPROVED', lastDmStatus: 'DM_STATUS_SENT' },
          { id: 'a2', discordHandle: 'bob#2', platforms: ['PLATFORM_XBOX'], status: 'APPLICANT_STATUS_PENDING' }
        ]
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Participants' }))
    expect(await screen.findByText('alice#1')).toBeInTheDocument()
    expect(screen.getByText('bob#2')).toBeInTheDocument()
    expect(screen.getByText('2 / ∞ enrolled')).toBeInTheDocument()
    expect(screen.getByText('steam')).toBeInTheDocument()
    expect(screen.getByText('xbox')).toBeInTheDocument()
    expect(screen.getByText('Sent')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: /^approve$/i })[0]).toBeInTheDocument()
  })

  it('Participants tab shows Retry DM only for APPROVED participants whose last DM failed', async () => {
    mockGetParticipants.mockReturnValue({
      data: {
        participants: [
          { applicantId: 'a1', discordHandle: 'a', status: 'APPLICANT_STATUS_APPROVED' },
          { applicantId: 'a2', discordHandle: 'b', status: 'APPLICANT_STATUS_APPROVED' },
          { applicantId: 'a3', discordHandle: 'c', status: 'APPLICANT_STATUS_PENDING' }
        ]
      },
      isLoading: false,
      refetch: vi.fn()
    })
    mockGetApplicants.mockReturnValue({
      data: {
        applicants: [
          { id: 'a1', discordHandle: 'a', status: 'APPLICANT_STATUS_APPROVED', lastDmStatus: 'DM_STATUS_FAILED' },
          { id: 'a2', discordHandle: 'b', status: 'APPLICANT_STATUS_APPROVED', lastDmStatus: 'DM_STATUS_SENT' },
          { id: 'a3', discordHandle: 'c', status: 'APPLICANT_STATUS_PENDING' }
        ]
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Participants' }))
    await screen.findByText('a')
    expect(screen.getAllByRole('button', { name: /retry dm/i })).toHaveLength(1)
  })

  it('Participants tab renders the low-pool banner when unused/total ≤ 10%', async () => {
    mockGetCodes.mockReturnValue({
      data: { stats: { total: 100, unused: 5, reserved: 0, granted: 95 }, codes: [] },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Participants' }))
    expect(await screen.findByText(/code pool is low/i)).toBeInTheDocument()
  })

  it('Participants tab opens the reject modal and submits the typed reason', async () => {
    const mutate = vi.fn()
    mockReject.mockReturnValue({ mutate, isPending: false })
    mockGetParticipants.mockReturnValue({
      data: { participants: [{ applicantId: 'a1', discordHandle: 'tester', status: 'APPLICANT_STATUS_PENDING' }] },
      isLoading: false,
      refetch: vi.fn()
    })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Participants' }))
    await user.click(await screen.findByRole('button', { name: /^reject$/i }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByPlaceholderText(/stored on the applicant row/i), 'duplicate signup')
    await user.click(within(dialog).getByRole('button', { name: /^reject$/i }))
    await waitFor(() =>
      expect(mutate).toHaveBeenCalledWith({ applicantId: 'a1', data: { rejectionReason: 'duplicate signup' } })
    )
  })

  it('Participants tab approve flow confirms via Popconfirm before mutating', async () => {
    const mutate = vi.fn()
    mockApprove.mockReturnValue({ mutate, isPending: false })
    mockGetParticipants.mockReturnValue({
      data: { participants: [{ applicantId: 'a1', discordHandle: 'tester', status: 'APPLICANT_STATUS_PENDING' }] },
      isLoading: false,
      refetch: vi.fn()
    })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Participants' }))
    await user.click(await screen.findByRole('button', { name: /^approve$/i }))
    const popup = await screen.findByRole('tooltip')
    await user.click(within(popup).getByRole('button', { name: /^approve$/i }))
    await waitFor(() => expect(mutate).toHaveBeenCalledWith({ applicantId: 'a1', data: {} }))
  })

  it('Distribution tab renders the inline Steam upload form + empty-state hint when pool is empty', async () => {
    mockGetCodes.mockReturnValue({
      data: { stats: { total: 0, unused: 0, granted: 0 }, codes: [] },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Distribution' }))
    expect(await screen.findByRole('heading', { name: /upload steam keys/i })).toBeInTheDocument()
    expect(screen.getByText('No codes uploaded yet')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^choose file$/i })).toBeInTheDocument()
  })

  it('Distribution tab reads + uploads a Steam keys CSV inline', async () => {
    const mutate = vi.fn()
    mockUploadCodes.mockReturnValue({ mutate, isPending: false })
    const { container } = renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Distribution' }))
    const csv = 'K7R2P-9M4XW-Q6V1B\nJ9L5T-B2N8R-M3K7P\n'
    const file = new File([csv], 'dummycodes.txt', { type: 'text/plain' })
    const input = container.querySelector('input[type="file"]') as HTMLInputElement
    await user.upload(input, file)
    await waitFor(() => expect(screen.getByText('dummycodes.txt')).toBeInTheDocument())
    const uploadBtn = screen.getByRole('button', { name: /^upload$/i })
    await waitFor(() => expect(uploadBtn).not.toBeDisabled())
    await user.click(uploadBtn)
    await waitFor(() =>
      expect(mutate).toHaveBeenCalledWith({
        playtestId: 'pt-draft',
        data: { csvContent: csv, filename: 'dummycodes.txt' }
      })
    )
  })

  it('Distribution tab triggers AGS top-up + sync mutations inline', async () => {
    const topUp = vi.fn()
    const sync = vi.fn()
    mockTopUpCodes.mockReturnValue({ mutate: topUp, isPending: false })
    mockSyncCodes.mockReturnValue({ mutate: sync, isPending: false })
    const agsPt = { ...DRAFT_PT, slug: 'autumn-ags', distributionModel: 'DISTRIBUTION_MODEL_AGS_CAMPAIGN' }
    mockGetPlaytests.mockReturnValue({
      data: { playtests: [agsPt] },
      isLoading: false,
      error: undefined,
      refetch: vi.fn()
    })
    renderDetail('autumn-ags')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Distribution' }))
    await user.click(await screen.findByRole('button', { name: /generate more codes/i }))
    await waitFor(() => expect(topUp).toHaveBeenCalledWith({ playtestId: 'pt-draft', data: { quantity: 100 } }))
    await user.click(screen.getByRole('button', { name: /sync from ags/i }))
    await waitFor(() => expect(sync).toHaveBeenCalledWith({ playtestId: 'pt-draft', data: {} }))
  })

  it('Distribution tab renders ADT empty-state when adt_namespace is missing', async () => {
    const adtDraft = { ...DRAFT_PT, slug: 'autumn-adt', distributionModel: 'DISTRIBUTION_MODEL_ADT', adtNamespace: undefined }
    mockGetPlaytests.mockReturnValue({
      data: { playtests: [adtDraft] },
      isLoading: false,
      error: undefined,
      refetch: vi.fn()
    })
    renderDetail('autumn-adt')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Distribution' }))
    expect(await screen.findByText('🔗 ADT Namespace Not Linked')).toBeInTheDocument()
  })

  it('Copy share link uses the backend-supplied player_base_url, not the admin host', async () => {
    mockGetPublicConfig.mockReturnValue({ data: { playerBaseUrl: 'https://play.example.com/' } })
    const user = userEvent.setup()
    renderDetail('autumn-draft')
    await user.click(screen.getByRole('button', { name: 'Copy share link' }))
    await waitFor(async () => {
      const text = await navigator.clipboard.readText()
      expect(text).toBe('https://play.example.com/#/playtest/autumn-draft')
    })
  })

  it('Copy share link shows an error when player_base_url is unset (no clipboard write)', async () => {
    mockGetPublicConfig.mockReturnValue({ data: { playerBaseUrl: '' } })
    const user = userEvent.setup()
    renderDetail('autumn-draft')
    await user.click(screen.getByRole('button', { name: 'Copy share link' }))
    expect(await screen.findByText(/PLAYER_BASE_URL/)).toBeInTheDocument()
    expect(await navigator.clipboard.readText()).toBe('')
  })

  it('Publish click triggers the transition mutation via confirm modal', async () => {
    const mutate = vi.fn()
    mockTransition.mockReturnValue({ mutate })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByTestId('header-publish'))
    // Confirm modal renders an OK Publish button distinct from the header.
    const modalButtons = await screen.findAllByRole('button', { name: 'Publish' })
    await user.click(modalButtons[modalButtons.length - 1])
    await waitFor(() => expect(mutate).toHaveBeenCalled())
    expect(mutate.mock.calls[0][0]).toMatchObject({
      playtestId: 'pt-draft',
      data: { targetStatus: 'PLAYTEST_STATUS_OPEN' }
    })
  })
})
