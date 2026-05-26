import { getLocale, setLocale, locales, type Locale } from "@/paraglide/runtime";

export type { Locale };

export const localeNames: Record<Locale, string> = {
  en: "English",
  fr: "Français",
  de: "Deutsch",
  es: "Español",
};

export const availableLocales = locales as readonly Locale[];

export function currentLocale(): Locale {
  return getLocale();
}

export function changeLocale(locale: Locale) {
  setLocale(locale);
}
