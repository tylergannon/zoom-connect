import { createContext } from 'svelte';
import type { User } from './types';

/** The signed-in person, provided by the root layout to every app page. */
export const [getUser, setUser] = createContext<() => User>();

/** The company domain, taken from the signed-in person's email. */
export function domainOf(user: User) {
	return user.email.split('@')[1] ?? '';
}
