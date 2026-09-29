import { createContext, useContext } from 'react'

export interface ToastApi {
  show: (text: string, link?: { to: string; label: string }) => void
}

export const ToastContext = createContext<ToastApi>({ show: () => undefined })

export function useToast(): ToastApi {
  return useContext(ToastContext)
}
