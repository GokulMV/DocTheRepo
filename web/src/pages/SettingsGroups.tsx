import { useEffect } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { useMe } from '@/api/hooks';
import { atLeast } from '@/api/types';
import SettingsFile from './SettingsFile';
import SignIn from './SignIn';
import Spend from './Spend';
import Users from './Users';

/** People is Settings → People: who may use the Hub and, for owners, how they sign in. */
export function People() {
  const me = useMe();
  const loc = useLocation();
  const owner = atLeast(me.data?.role, 'owner');
  // /sign-in opens People at its sign-in part.
  useEffect(() => {
    if (loc.pathname === '/sign-in' && owner) setTimeout(() => document.getElementById('sign-in')?.scrollIntoView(), 50);
  }, [loc.pathname, owner]);
  return (
    <>
      <Users />
      {owner && (
        <div id="sign-in" className="mt-12 border-t border-slate-200/80 pt-8 dark:border-white/[0.07]">
          <SignIn />
        </div>
      )}
    </>
  );
}

/** Advanced is Settings → Advanced: detailed spend limits, the settings file, and the setup checklist. */
export function Advanced() {
  return (
    <>
      <Spend />
      <div className="mt-12 border-t border-slate-200/80 pt-8 dark:border-white/[0.07]">
        <SettingsFile />
      </div>
      <p className="mt-8 text-sm text-slate-500">
        Setting up a new Hub? The <Link className="text-brand-600 underline dark:text-brand-300" to="/setup">setup checklist</Link> shows what is left.
      </p>
    </>
  );
}
