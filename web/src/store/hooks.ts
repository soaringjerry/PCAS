import { useCallback } from 'react'
import { useNavigate } from 'react-router'
import { draftSections, memoriesFor } from '../domain/handoff'
import { newId } from '../domain/ids'
import { nowIso } from '../domain/time'
import type { Handoff } from '../domain/types'
import { useStore } from './context'

interface HandoffTarget {
  projectId?: string
  taskId?: string
  ideaId?: string
}

/** Draft a handoff from the current state and open it for editing. */
export function useCreateHandoff() {
  const { state, dispatch } = useStore()
  const navigate = useNavigate()
  return useCallback(
    (target: HandoffTarget, agentId = 'a_claude') => {
      const task = state.tasks.find((t) => t.id === target.taskId)
      // A task promoted from an idea keeps the idea's background.
      const idea = state.ideas.find((i) => i.id === (target.ideaId ?? task?.ideaId))
      const projectId = target.projectId ?? task?.projectId ?? idea?.projectId
      const agent = state.agents.find((a) => a.id === agentId) ?? state.agents[0]
      const memories = memoriesFor(state, agent, projectId)
      const at = nowIso()
      const handoff: Handoff = {
        id: newId('h'),
        title: task?.title ?? idea?.title ?? state.projects.find((p) => p.id === projectId)?.name ?? '新的交接',
        agentId: agent.id,
        projectId,
        taskId: task?.id,
        ideaId: idea?.id,
        sections: draftSections(state, { projectId, task, idea }, memories),
        memoryIds: memories.map((m) => m.id),
        status: 'draft',
        stale: false,
        createdAt: at,
        updatedAt: at,
      }
      dispatch({ type: 'createHandoff', handoff })
      navigate(`/handoff/${handoff.id}`)
    },
    [state, dispatch, navigate],
  )
}
