"use client";

import { cn } from "@/lib/utils";
import { type ReactNode, useRef, useState } from "react";

interface TooltipProps {
  children: ReactNode;
  content: string;
  side?: "top" | "bottom" | "left" | "right";
  className?: string;
}

export function Tooltip({
  children,
  content: _content,
  side = "top",
  className,
}: TooltipProps) {
  const anchor = useRef<HTMLSpanElement>(null);
  const [shift, setShift] = useState(0);
  const position = () => {
    const rect = anchor.current?.getBoundingClientRect();
    if (!rect) return;
    const width = Math.min(208, window.innerWidth - 16);
    const left = side === "left" ? rect.left - width - 6 : side === "right" ? rect.right + 6 : rect.left + rect.width / 2 - width / 2;
    setShift(Math.max(8, Math.min(left, window.innerWidth - width - 8)) - left);
  };
  return (
    <span ref={anchor} onMouseEnter={position} onFocus={position} className="relative inline-flex group/tip">
      {children}
      <span
        role="tooltip"
        style={{ translate: `${shift}px 0` }}
        className={cn(
          "pointer-events-none absolute z-50 w-52 max-w-[calc(100vw-16px)] hidden group-hover/tip:block group-focus-within/tip:block",
          "bg-[var(--surface-base)] text-[var(--text-secondary)] text-[10px] leading-[14px] rounded-md px-2.5 py-1.5 shadow-[var(--shadow-elevated)]",
          "transition-opacity duration-150 opacity-0 group-hover/tip:opacity-100 group-focus-within/tip:opacity-100",
          "whitespace-normal break-words",
          side === "top" && "bottom-full left-1/2 -translate-x-1/2 mb-1.5",
          side === "bottom" && "top-full left-1/2 -translate-x-1/2 mt-1.5",
          side === "left" && "right-full top-1/2 -translate-y-1/2 mr-1.5",
          side === "right" && "left-full top-1/2 -translate-y-1/2 ml-1.5",
          className
        )}
      >
        {_content}
        <span
          className={cn(
            "pointer-events-none absolute",
            "border-4 border-transparent",
            side === "top" &&
              "top-full left-1/2 -translate-x-1/2 border-t-[var(--surface-base)]",
            side === "bottom" &&
              "bottom-full left-1/2 -translate-x-1/2 border-b-[var(--surface-base)]",
            side === "left" &&
              "left-full top-1/2 -translate-y-1/2 border-l-[var(--surface-base)]",
            side === "right" &&
              "right-full top-1/2 -translate-y-1/2 border-r-[var(--surface-base)]"
          )}
        />
      </span>
    </span>
  );
}

interface HelpTooltipProps {
  content: string;
}

export function HelpTooltip({ content }: HelpTooltipProps) {
  return (
    <Tooltip content={content} side="bottom">
      <button
        type="button"
        className="inline-flex items-center justify-center size-5 rounded-full bg-[var(--surface-muted)] text-[var(--text-muted)] text-[9px] cursor-help hover:bg-[var(--surface-hover)] hover:text-[var(--text-secondary)] transition-colors duration-150 select-none ml-1 align-middle"
        aria-label={content}
      >
        ?
      </button>
    </Tooltip>
  );
}
