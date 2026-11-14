import { createContext, useContext, useState, type ButtonHTMLAttributes, type HTMLAttributes, type ReactNode } from 'react'
import { Slot } from '@radix-ui/react-slot'
import { PanelLeft } from 'lucide-react'
import { cn } from '@/lib/utils'

type SidebarContextValue = { open: boolean; toggle: () => void }
const SidebarContext = createContext<SidebarContextValue | null>(null)

export function SidebarProvider({ children, defaultOpen = true }: { children: ReactNode; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen)
  return <SidebarContext.Provider value={{ open, toggle: () => setOpen((value) => !value) }}>{children}</SidebarContext.Provider>
}

export function useSidebar() {
  const context = useContext(SidebarContext)
  if (!context) throw new Error('useSidebar debe usarse dentro de SidebarProvider')
  return context
}

export function Sidebar({
  children,
  variant = 'sidebar',
  collapsible = 'icon'
}: {
  children: ReactNode
  variant?: 'sidebar' | 'floating' | 'inset'
  collapsible?: 'offcanvas' | 'icon' | 'none'
}) {
  const { open } = useSidebar()
  return (
    <aside
      data-slot="sidebar"
      data-state={open ? 'expanded' : 'collapsed'}
      data-variant={variant}
      data-collapsible={open || collapsible === 'none' ? '' : collapsible}
      className={cn('app-sidebar group/sidebar flex shrink-0 flex-col overflow-hidden transition-[width] duration-200', open ? 'w-[252px]' : 'w-16')}
    >
      {children}
    </aside>
  )
}

export function SidebarInset({ className, ...props }: HTMLAttributes<HTMLElement>) {
  return (
    <main
      data-slot="sidebar-inset"
      className={cn('sidebar-inset app-main-panel mb-2 mr-2 mt-0 ml-0 flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden', className)}
      {...props}
    />
  )
}

export function SidebarTrigger({ className, ...props }: ButtonHTMLAttributes<HTMLButtonElement>) {
  const { open, toggle } = useSidebar()
  return (
    <button
      type="button"
      data-slot="sidebar-trigger"
      aria-label={open ? 'Contraer sidebar' : 'Expandir sidebar'}
      title={open ? 'Contraer sidebar' : 'Expandir sidebar'}
      onClick={toggle}
      onDoubleClick={(event) => event.stopPropagation()}
      className={cn('sidebar-trigger grid size-8 shrink-0 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary', className)}
      {...props}
    >
      <PanelLeft className="size-4" />
    </button>
  )
}

export function SidebarHeader({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div data-slot="sidebar-header" className={cn('sidebar-header flex h-14 shrink-0 items-center gap-2.5 border-b border-sidebar-foreground/[0.06] px-4', className)} {...props} />
}

export function SidebarContent({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div data-slot="sidebar-content" className={cn('scrollbar-thin flex min-h-0 flex-1 flex-col overflow-y-auto overflow-x-hidden', className)} {...props} />
}

export function SidebarFooter({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div data-slot="sidebar-footer" className={cn('sidebar-footer shrink-0 border-t border-sidebar-foreground/[0.06] p-2', className)} {...props} />
}

export function SidebarGroup({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <section data-slot="sidebar-group" className={cn('sidebar-group min-w-0 px-2 py-2', className)} {...props} />
}

export function SidebarGroupLabel({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div data-slot="sidebar-group-label" className={cn('sidebar-label px-2.5 pb-2 pt-3 text-[10px] font-semibold tracking-[0.12em] text-sidebar-foreground/45 uppercase', className)} {...props} />
}

export function SidebarGroupAction({ className, ...props }: ButtonHTMLAttributes<HTMLButtonElement>) {
  return <button data-slot="sidebar-group-action" className={cn('sidebar-group-action grid size-8 shrink-0 place-items-center rounded-md text-sidebar-foreground/55 transition-colors hover:bg-sidebar-foreground/[0.07] hover:text-sidebar-foreground', className)} {...props} />
}

export function SidebarMenu({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div data-slot="sidebar-menu" className={cn('flex min-w-0 flex-col gap-1', className)} {...props} />
}

export function SidebarMenuButton({
  asChild,
  isActive = false,
  tooltip,
  className,
  children,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { asChild?: boolean; isActive?: boolean; tooltip?: string }) {
  const { open } = useSidebar()
  const Component = asChild ? Slot : 'button'
  return (
    <Component
      data-slot="sidebar-menu-button"
      data-active={isActive}
      data-size="default"
      title={!open ? tooltip : undefined}
      className={cn('sidebar-menu-button flex h-9 w-full min-w-0 items-center gap-2.5 overflow-hidden rounded-md px-2.5 text-left text-[13px] text-sidebar-foreground/80 transition-colors hover:bg-sidebar-foreground/[0.06] hover:text-sidebar-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary', className)}
      {...props}
    >
      {children}
    </Component>
  )
}
