import { isString } from "./common.js";
import { SYN_COLOR_TYPE, SYN_MIX, VAL_SPEC } from "./constant.js";
import { NAMED_COLORS } from "./color.js";
import { resolveColor } from "./resolve.js";
import { CacheItem, createCacheKey, getCache, setCache } from "./cache.js";
import { TokenType, tokenize } from "@csstools/css-tokenizer";
//#region src/js/util.ts
/**
* util
*/
var { CloseParen: PAREN_CLOSE, Comma: COMMA, Comment: COMMENT, Delim: DELIM, EOF, Function: FUNC, Ident: IDENT, OpenParen: PAREN_OPEN, Whitespace: W_SPACE } = TokenType;
var NAMESPACE = "util";
var DEC = 10;
var HEX = 16;
var DEG = 360;
var DEG_HALF = 180;
var REG_COLOR = new RegExp(`^(?:${SYN_COLOR_TYPE})$`);
var REG_FN_COLOR = /^(?:(?:ok)?l(?:ab|ch)|color(?:-mix)?|hsla?|hwb|rgba?|var)\(/;
var REG_MIX = new RegExp(SYN_MIX);
/**
* split value
* NOTE: comments are stripped, it can be preserved if, in the options param,
* `delimiter` is either ',' or '/' and with `preserveComment` set to `true`
* @param value - CSS value
* @param [opt] - options
* @returns array of values
*/
var splitValue = (value, opt = {}) => {
	if (isString(value)) value = value.trim();
	else throw new TypeError(`${value} is not a string.`);
	const { delimiter = " ", preserveComment = false } = opt;
	const cacheKey = createCacheKey({
		namespace: NAMESPACE,
		name: "splitValue",
		value
	}, {
		delimiter,
		preserveComment
	});
	const cachedResult = getCache(cacheKey);
	if (cachedResult instanceof CacheItem) return cachedResult.item;
	let regDelimiter;
	if (delimiter === ",") regDelimiter = /^,$/;
	else if (delimiter === "/") regDelimiter = /^\/$/;
	else regDelimiter = /^\s+$/;
	const tokens = tokenize({ css: value });
	let nest = 0;
	let str = "";
	const res = [];
	while (tokens.length) {
		const [type, value] = tokens.shift();
		switch (type) {
			case COMMA:
				if (regDelimiter.test(value)) if (nest === 0) {
					res.push(str.trim());
					str = "";
				} else str += value;
				else str += value;
				break;
			case DELIM:
				if (regDelimiter.test(value)) if (nest === 0) {
					res.push(str.trim());
					str = "";
				} else str += value;
				else str += value;
				break;
			case COMMENT:
				if (preserveComment && (delimiter === "," || delimiter === "/")) str += value;
				break;
			case FUNC:
			case PAREN_OPEN:
				str += value;
				nest++;
				break;
			case PAREN_CLOSE:
				str += value;
				nest--;
				break;
			case W_SPACE:
				if (regDelimiter.test(value)) if (nest === 0) {
					if (str) {
						res.push(str.trim());
						str = "";
					}
				} else str += " ";
				else if (!str.endsWith(" ")) str += " ";
				break;
			default: if (type === EOF) {
				res.push(str.trim());
				str = "";
			} else str += value;
		}
	}
	setCache(cacheKey, res);
	return res;
};
/**
* extract dashed-ident tokens
* @param value - CSS value
* @returns array of dashed-ident tokens
*/
var extractDashedIdent = (value) => {
	if (isString(value)) value = value.trim();
	else throw new TypeError(`${value} is not a string.`);
	const cacheKey = createCacheKey({
		namespace: NAMESPACE,
		name: "extractDashedIdent",
		value
	});
	const cachedResult = getCache(cacheKey);
	if (cachedResult instanceof CacheItem) return cachedResult.item;
	const tokens = tokenize({ css: value });
	const items = /* @__PURE__ */ new Set();
	while (tokens.length) {
		const [type, value] = tokens.shift();
		if (type === IDENT && value.startsWith("--")) items.add(value);
	}
	const res = [...items];
	setCache(cacheKey, res);
	return res;
};
/**
* is color
* @param value - CSS value
* @param [opt] - options
* @returns result
*/
var isColor = (value, opt = {}) => {
	if (isString(value)) {
		value = value.toLowerCase().trim();
		if (value && isString(value)) {
			if (/^[a-z]+$/.test(value)) {
				if (/^(?:currentcolor|transparent)$/.test(value) || Object.hasOwn(NAMED_COLORS, value)) return true;
			} else if (REG_COLOR.test(value) || REG_MIX.test(value)) return true;
			else if (REG_FN_COLOR.test(value)) {
				opt.nullable = true;
				if (!opt.format) opt.format = VAL_SPEC;
				if (resolveColor(value, opt)) return true;
			}
		}
	}
	return false;
};
/**
* value to JSON string
* @param value - CSS value
* @param [func] - stringify function
* @returns stringified value in JSON notation
*/
var valueToJsonString = (value, func = false) => {
	if (typeof value === "undefined") return "";
	return JSON.stringify(value, (_key, val) => {
		let replacedValue;
		if (typeof val === "undefined") replacedValue = null;
		else if (typeof val === "function") if (func) replacedValue = val.toString().replace(/\s/g, "").substring(0, HEX);
		else replacedValue = val.name;
		else if (val instanceof Map || val instanceof Set) replacedValue = [...val];
		else if (typeof val === "bigint") replacedValue = val.toString();
		else replacedValue = val;
		return replacedValue;
	});
};
/**
* round to specified precision
* @param value - numeric value
* @param bit - minimum bits
* @returns rounded value
*/
var roundToPrecision = (value, bit = 0) => {
	if (!Number.isFinite(value)) throw new TypeError(`${value} is not a finite number.`);
	if (!Number.isFinite(bit)) throw new TypeError(`${bit} is not a finite number.`);
	else if (bit < 0 || bit > HEX) throw new RangeError(`${bit} is not between 0 and ${HEX}.`);
	if (bit === 0) return Math.round(value);
	let val;
	if (bit === HEX) val = value.toPrecision(6);
	else if (bit < DEC) val = value.toPrecision(4);
	else val = value.toPrecision(5);
	return parseFloat(val);
};
/**
* interpolate hue
* @param hueA - hue value
* @param hueB - hue value
* @param arc - shorter | longer | increasing | decreasing
* @returns result - [hueA, hueB]
*/
var interpolateHue = (hueA, hueB, arc = "shorter") => {
	if (!Number.isFinite(hueA)) throw new TypeError(`${hueA} is not a finite number.`);
	if (!Number.isFinite(hueB)) throw new TypeError(`${hueB} is not a finite number.`);
	switch (arc) {
		case "decreasing":
			if (hueB > hueA) hueA += DEG;
			break;
		case "increasing":
			if (hueB < hueA) hueB += DEG;
			break;
		case "longer":
			if (hueB > hueA && hueB < hueA + DEG_HALF) hueA += DEG;
			else if (hueB > hueA + DEG_HALF * -1 && hueB <= hueA) hueB += DEG;
			break;
		default: if (hueB > hueA + DEG_HALF) hueA += DEG;
		else if (hueB < hueA + DEG_HALF * -1) hueB += DEG;
	}
	return [hueA, hueB];
};
var absoluteFontSize = new Map([
	["xx-small", 9 / 16],
	["x-small", 5 / 8],
	["small", 13 / 16],
	["medium", 1],
	["large", 9 / 8],
	["x-large", 3 / 2],
	["xx-large", 2],
	["xxx-large", 3]
]);
var relativeFontSize = new Map([["smaller", 1 / 1.2], ["larger", 1.2]]);
var absoluteLength = new Map([
	["cm", 96 / 2.54],
	["mm", 96 / 2.54 / 10],
	["q", 96 / 2.54 / 40],
	["in", 96],
	["pc", 96 / 6],
	["pt", 96 / 72],
	["px", 1]
]);
var relativeLength = new Map([
	["rcap", 1],
	["rch", .5],
	["rem", 1],
	["rex", .5],
	["ric", 1],
	["rlh", 1.2]
]);
/**
* resolve length in pixels
* @param value - value
* @param unit - unit
* @param [opt] - options
* @returns pixelated value
*/
var resolveLengthInPixels = (value, unit, opt = {}) => {
	const { dimension = {} } = opt;
	const { callback, em, rem, vh, vw } = dimension;
	if (isString(value)) {
		value = value.toLowerCase().trim();
		if (absoluteFontSize.has(value)) return Number(absoluteFontSize.get(value)) * rem;
		else if (relativeFontSize.has(value)) return Number(relativeFontSize.get(value)) * em;
		return NaN;
	} else if (Number.isFinite(value) && unit) if (Object.hasOwn(dimension, unit)) return value * Number(dimension[unit]);
	else if (typeof callback === "function") return value * callback(unit);
	else if (absoluteLength.has(unit)) return value * Number(absoluteLength.get(unit));
	else if (relativeLength.has(unit)) return value * Number(relativeLength.get(unit)) * rem;
	else if (relativeLength.has(`r${unit}`)) return value * Number(relativeLength.get(`r${unit}`)) * em;
	else switch (unit) {
		case "vb":
		case "vi": return value * vw;
		case "vmax":
			if (vh > vw) return value * vh;
			return value * vw;
		case "vmin":
			if (vh < vw) return value * vh;
			return value * vw;
		default: return NaN;
	}
	return NaN;
};
/**
* is absolute size or length
* @param value - value
* @param unit - unit
* @returns result
*/
var isAbsoluteSizeOrLength = (value, unit) => {
	if (isString(value)) {
		value = value.toLowerCase().trim();
		return absoluteFontSize.has(value);
	} else if (isString(unit)) {
		unit = unit.toLowerCase().trim();
		return absoluteLength.has(unit);
	}
	return value === 0;
};
/**
* is absolute font size
* @param value - value
* @returns result
*/
var isAbsoluteFontSize = (value) => {
	if (isString(value)) {
		const size = value.toLowerCase().trim();
		if (/^[a-z-]+$/.test(size)) return absoluteFontSize.has(size);
		else {
			const [, val, unit] = /^(\d+(?:\.\d+)?|\.\d+)([a-z-]+)?$/.exec(size);
			if (unit) return absoluteLength.has(unit);
			else if (val) return parseFloat(val) === 0;
		}
	}
	return false;
};
//#endregion
export { extractDashedIdent, interpolateHue, isAbsoluteFontSize, isAbsoluteSizeOrLength, isColor, resolveLengthInPixels, roundToPrecision, splitValue, valueToJsonString };

//# sourceMappingURL=util.js.map