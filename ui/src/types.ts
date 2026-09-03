export type Role = 'user' | 'assistant'

export interface Message {
  id: string
  role: Role
  text: string
}

/** A retrieved paper, shown in the sidebar. Nothing produces these yet. */
export interface Source {
  pmcid: string
  pmid?: string
  title: string
  journal?: string
  year?: number
  confidence?: number
}
