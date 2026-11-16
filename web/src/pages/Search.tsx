import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { SongCell, songSubtitle } from '../components/SongList'
import { CellSkeleton, Chips, Empty, ErrorState, SearchField } from '../components/ui'
import { get, query, type Me, type SearchResults } from '../lib/api'
import { formatTrack } from '../lib/format'
import { refKey, useSendSong } from '../lib/hooks'
import { t } from '../lib/i18n'

export function Search({ me, hidden }: { me: Me; hidden: boolean }) {
  const [input, setInput] = useState('')
  const [keyword, setKeyword] = useState('')
  const [provider, setProvider] = useState(() => me.providers.find((p) => p.default)?.name ?? '')
  const { send, sending } = useSendSong()

  // 接口一次返回全部结果（bot 最多展示的条数）
  const results = useQuery({
    queryKey: ['search', provider, keyword],
    queryFn: () => get<SearchResults>('/search' + query({ q: keyword, provider })),
    enabled: keyword !== '',
    staleTime: 5 * 60 * 1000,
    retry: false,
  })
  const items = results.data?.items ?? []

  return (
    <div className="page tab-in" hidden={hidden}>
      <h1 className="large-title">{t.search.title}</h1>
      <div className="sticky-bar">
        <SearchField
          value={input}
          onChange={(value) => {
            setInput(value)
            if (value === '') setKeyword('')
          }}
          onSubmit={() => setKeyword(input.trim())}
          placeholder={t.search.placeholder}
        />
        {me.providers.length > 1 && (
          <Chips
            value={provider}
            onChange={setProvider}
            options={me.providers.map((p) => ({ value: p.name, label: p.display }))}
          />
        )}
      </div>

      {keyword === '' ? (
        <Empty icon="search" title={t.search.idle}>
          {t.search.idleHint}
        </Empty>
      ) : results.isPending ? (
        <CellSkeleton rows={8} />
      ) : results.isError ? (
        <ErrorState error={results.error} onRetry={results.refetch} />
      ) : items.length === 0 ? (
        <Empty icon="search" title={t.search.none}>
          {t.search.noneHint}
        </Empty>
      ) : (
        <div className="card">
          {items.map((item) => (
            <SongCell
              key={refKey(item)}
              title={item.title}
              subtitle={songSubtitle(item)}
              busy={sending === refKey(item)}
              onClick={() => send({ provider: item.provider, id: item.id, sub: item.sub })}
              side={
                <>
                  {item.cached && <span className="badge accent">{t.search.cached}</span>}
                  {item.duration > 0 && <span className="num">{formatTrack(item.duration)}</span>}
                </>
              }
            />
          ))}
        </div>
      )}
    </div>
  )
}
