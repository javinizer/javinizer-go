import { valueToJsonString } from "./util.js";
import { LRUCache } from "lru-cache";
//#region src/js/cache.ts
/**
* cache
*/
var MAX_CACHE = 4096;
/**
* CacheItem
*/
var CacheItem = class {
	#isNull;
	#item;
	/**
	* constructor
	*/
	constructor(item, isNull = false) {
		this.#item = item;
		this.#isNull = !!isNull;
	}
	get item() {
		return this.#item;
	}
	get isNull() {
		return this.#isNull;
	}
};
/**
* NullObject
*/
var NullObject = class extends CacheItem {
	/**
	* constructor
	*/
	constructor() {
		super(Symbol("null"), true);
	}
};
var lruCache = new LRUCache({ max: MAX_CACHE });
/**
* set cache
* @param key - cache key
* @param value - value to cache
* @returns void
*/
var setCache = (key, value) => {
	if (key) if (value === null) lruCache.set(key, new NullObject());
	else if (value instanceof CacheItem) lruCache.set(key, value);
	else lruCache.set(key, new CacheItem(value));
};
/**
* get cache
* @param key - cache key
* @returns cached item or false otherwise
*/
var getCache = (key) => {
	if (key && lruCache.has(key)) {
		const item = lruCache.get(key);
		if (item instanceof CacheItem) return item;
		lruCache.delete(key);
		return false;
	}
	return false;
};
/**
* create cache key
* @param keyData - key data
* @param [opt] - options
* @returns cache key
*/
var createCacheKey = (keyData, opt = {}) => {
	const { customProperty = {}, dimension = {} } = opt;
	let cacheKey = "";
	if (keyData && Object.keys(keyData).length && typeof customProperty.callback !== "function" && typeof dimension.callback !== "function") {
		keyData.opt = valueToJsonString(opt);
		cacheKey = valueToJsonString(keyData);
	}
	return cacheKey;
};
//#endregion
export { CacheItem, NullObject, createCacheKey, getCache, setCache };

//# sourceMappingURL=cache.js.map