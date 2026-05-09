'use client';

import { useEffect } from 'react';

interface Props {
  open: boolean;
  onClose: () => void;
  title?: string;
  message?: string;
  contact?: string;
}

// ContactTeamModal is a centered overlay shown to non-admin users who try to
// perform an admin-only action (adding a team member, creating an
// organization, etc.). The backend enforces the restriction; this just gives
// a friendlier UX than a raw 403 by pointing the user to the right person.
export function ContactTeamModal({
  open,
  onClose,
  title = 'Admin action required',
  message = 'Adding team members is restricted to admins. Please ask an admin on your team to invite them, or reach out to us and we will help.',
  contact = 'support@gateway-llm.com',
}: Props) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm"
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-labelledby="contact-team-title"
    >
      <div
        className="gatewayllm-card mx-4 w-full max-w-md p-6"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start gap-3">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-amber-900/40 ring-1 ring-amber-700/50">
            <svg
              xmlns="http://www.w3.org/2000/svg"
              fill="none"
              viewBox="0 0 24 24"
              strokeWidth="1.5"
              stroke="currentColor"
              className="h-5 w-5 text-amber-300"
              aria-hidden
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M12 9v3.75m0 3.75h.008v.008H12v-.008ZM2.697 16.126c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126Z"
              />
            </svg>
          </div>
          <div className="flex-1">
            <h3 id="contact-team-title" className="text-base font-medium text-white">
              {title}
            </h3>
            <p className="mt-2 text-sm text-zinc-300">{message}</p>
            <p className="mt-3 text-sm text-zinc-400">
              Contact us at{' '}
              <a
                href={`mailto:${contact}`}
                className="text-emerald-400 hover:text-emerald-300"
              >
                {contact}
              </a>
              .
            </p>
          </div>
        </div>
        <div className="mt-6 flex justify-end">
          <button type="button" className="gatewayllm-btn" onClick={onClose}>
            Got it
          </button>
        </div>
      </div>
    </div>
  );
}
