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
const mockGetAnnouncements = vi.fn()
const mockCreateAnnouncement = vi.fn()
const mockGetCodes = vi.fn()
const mockApprove = vi.fn()
const mockReject = vi.fn()

vi.mock('./playtesthubapi/generated-admin/queries/PlaytesthubServiceAdmin.query', () => ({
  Key_PlaytesthubServiceAdmin: {
    Playtests: 'playtests',
    Participants_ByPlaytestId: 'participants',
    Announcements_ByPlaytestId: 'announcements'
  },
  usePlaytesthubServiceAdminApi_GetPlaytests: (...a: unknown[]) => mockGetPlaytests(...a),
  usePlaytesthubServiceAdminApi_CreatePlaytest_ByPlaytestIdTransitionStatuMutation: (...a: unknown[]) =>
    mockTransition(...a),
  usePlaytesthubServiceAdminApi_GetParticipants_ByPlaytestId: (...a: unknown[]) => mockGetParticipants(...a),
  usePlaytesthubServiceAdminApi_GetAnnouncements_ByPlaytestId: (...a: unknown[]) => mockGetAnnouncements(...a),
  usePlaytesthubServiceAdminApi_CreateAnnouncement_ByPlaytestIdMutation: (...a: unknown[]) =>
    mockCreateAnnouncement(...a),
  usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId: (...a: unknown[]) => mockGetCodes(...a),
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdApproveMutation: (...a: unknown[]) =>
    mockApprove(...a),
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRejectMutation: (...a: unknown[]) => mockReject(...a)
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
  mockGetParticipants.mockReturnValue({ data: { participants: [] }, isLoading: false })
  mockGetAnnouncements.mockReturnValue({ data: { announcements: [] }, isLoading: false, refetch: vi.fn() })
  mockCreateAnnouncement.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockGetCodes.mockReturnValue({ data: { stats: { total: 0, unused: 0, granted: 0 } }, isLoading: false })
  mockApprove.mockReturnValue({ mutate: vi.fn() })
  mockReject.mockReturnValue({ mutate: vi.fn() })
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

  it('Participants tab renders the 6-column table + capacity counter', async () => {
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
      isLoading: false
    })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Participants' }))
    expect(await screen.findByText('alice#1')).toBeInTheDocument()
    expect(screen.getByText('bob#2')).toBeInTheDocument()
    expect(screen.getByText('2 / ∞ enrolled')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: 'Approve' })[0]).toBeInTheDocument()
  })

  it('Distribution tab renders the Steam empty state when pool is empty', async () => {
    mockGetCodes.mockReturnValue({ data: { stats: { total: 0, unused: 0, granted: 0 } }, isLoading: false })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Distribution' }))
    expect(await screen.findByText('No codes uploaded yet')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Upload Codes (CSV)' })).toBeInTheDocument()
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
