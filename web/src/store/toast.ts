import { createContext, useContext } from 'react'

export interface ToastOptions {
  link?: { to: string; label: string }
  /** Adds 【撤销】; the toast then stays 8 seconds, paused while pointed at. */
  undo?: () => Promise<unknown>
}

export interface ToastApi {
  show: (text: string, options?: ToastOptions) => void
}

export const ToastContext = createContext<ToastApi>({ show: () => undefined })

export function useToast(): ToastApi {
  return useContext(ToastContext)
}
