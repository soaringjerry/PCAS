import { Link } from 'react-router'
import { Empty, Sheet } from '../components/ui'

export function NotFound() {
  return (
    <main className="page page-narrow">
      <Sheet pad>
        <Empty>
          这一页找不到了。<Link to="/">回到现在</Link>
        </Empty>
      </Sheet>
    </main>
  )
}
