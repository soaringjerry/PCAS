import { useEffect, useState } from 'react'
import { shapeInProgress, shapeSchedule, type InProgress, type Schedule } from '../domain/schedule'
import { civilDate } from '../domain/time'
import { useStore } from './context'
import { useRead, type Read } from './now'

/** 「这几天」 looks this many days past today, as it always has. */
export const SOON_DAYS = 3

/** Today's date in the workspace zone; it moves on by itself at midnight there. */
function useToday(timezone: string): string {
  const [today, setToday] = useState(() => civilDate(0, timezone))
  useEffect(() => {
    const check = () => setToday(civilDate(0, timezone))
    check()
    const timer = window.setInterval(check, 60_000)
    return () => window.clearInterval(timer)
  }, [timezone])
  return today
}

/** Today and the three days after it. Read again whenever the workspace moves, since a to-do or a memory may have changed a day. */
export function useSchedule(): Read<Schedule> {
  const { state } = useStore()
  const timezone = state.settings.timezone ?? 'UTC'
  useToday(timezone)
  return useRead(`/v1/workspace/schedule?from=${civilDate(0, timezone)}&to=${civilDate(SOON_DAYS, timezone)}`, shapeSchedule, state.revision)
}

/** The to-dos that have no time; how many are listed is the server's to say. */
export function useInProgress(): Read<InProgress> {
  return useRead('/v1/workspace/in-progress', shapeInProgress, useStore().state.revision)
}
