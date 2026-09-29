import { Link } from 'react-router'
import { Card, Empty } from '../components/ui'

export function NotFound() {
  return (
    <main className="page">
      <Card pad>
        <Empty>
          找不到这个页面。<Link to="/">回到今天</Link>
        </Empty>
      </Card>
    </main>
  )
}
