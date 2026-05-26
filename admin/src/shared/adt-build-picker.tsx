import { useAppUIContext } from '@accelbyte/sdk-extend-app-ui'
import { Card, Modal, Select, Space, Typography } from 'antd'
import { useMemo, useState } from 'react'
import type { V1AdtBuild } from '../playtesthubapi/generated-definitions/V1AdtBuild'
import type { V1AdtGame } from '../playtesthubapi/generated-definitions/V1AdtGame'
import { usePlaytesthubServiceAdminApi_GetBuildsAdt_ByAdtLinkageId } from '../playtesthubapi/generated-admin/queries/PlaytesthubServiceAdmin.query'

// ADTBuildPickerModal renders the namespace → game → version → build
// picker that B13 specs against docs/images/build-picker-mockup.png.
// Game dropdown lives at the top; versions are derived by grouping
// ListADTBuilds on Build.name (= game_version_name); the right rail
// renders per-platform cards. "Use This Build" lifts (gameId, buildId)
// to the parent form.
export function ADTBuildPickerModal({
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
