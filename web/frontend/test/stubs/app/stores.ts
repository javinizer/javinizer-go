import { readable } from "svelte/store";
export const page = readable({ url: { pathname: "/" } });
