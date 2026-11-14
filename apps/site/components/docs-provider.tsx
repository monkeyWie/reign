"use client";
import type { ReactNode } from "react";
import { RootProvider } from "fumadocs-ui/provider/next";
import { i18n, isLocale, switchLocale, type Locale } from "../lib/i18n";
import { useLocale } from "./locale";
import { i18nProvider, uiTranslations } from "fumadocs-ui/i18n";
import { useStaticSearch } from "fumadocs-core/search/client";
import { startNavigation } from "../lib/navigation-progress";
import {
  SearchDialog,
  SearchDialogClose,
  SearchDialogContent,
  SearchDialogHeader,
  SearchDialogIcon,
  SearchDialogInput,
  SearchDialogList,
  SearchDialogOverlay,
  type SharedProps,
} from "fumadocs-ui/components/dialog/search";

const translations = i18n
  .translations()
  .extend(uiTranslations())
  .add({
    en: { displayName: "English" },
    "zh-CN": {
      displayName: "简体中文",
      "Choose a language(language switcher)": "选择语言",
      "Choose a language(language switcher)(aria-label)": "选择语言",
      "Search(search trigger)": "搜索文档",
      "Search(search dialog)": "搜索文档",
      "Open Search(search trigger)(aria-label)": "搜索文档",
      "Close Search(search dialog)(aria-label)": "关闭搜索",
      "No results found(search dialog)": "没有找到结果",
      "Search failed(search dialog)": "搜索失败，请重试",
      "Search failed, showing results for {query}(search dialog)":
        "搜索失败，显示 {query} 的结果",
      "On this page(table of contents)": "本页内容",
      "No Headings(table of contents)": "本页没有目录",
      "Table of Contents(inline table of contents)": "本页目录",
      "Previous Page(pagination)": "上一篇",
      "Next Page(pagination)": "下一篇",
      "Last updated on(page footer)": "最后更新",
      "Edit on GitHub(edit page)": "在 GitHub 上编辑此页",
      "Toggle Menu(mobile menu)(aria-label)": "展开菜单",
      "Toggle Theme(theme switcher)(aria-label)": "切换主题",
      "Light(theme switcher)(aria-label)": "浅色主题",
      "Dark(theme switcher)(aria-label)": "深色主题",
      "System(theme switcher)(aria-label)": "跟随系统",
      "Open Sidebar(sidebar)(aria-label)": "打开目录",
      "Close Sidebar(sidebar)(aria-label)": "关闭目录",
      "Close Sidebar(aria-label)": "关闭目录",
      "Collapse Sidebar(sidebar)(aria-label)": "收起目录",
      "Hide Sidebar(sidebar)": "隐藏目录",
      "Show Sidebar(sidebar)": "显示目录",
      "Copy Text(code block)(aria-label)": "复制代码",
      "Copied Text(code block)(aria-label)": "已复制代码",
      "Copy Anchor Link(heading anchor)(aria-label)": "复制标题链接",
      "Copied Anchor Link(heading anchor)(aria-label)": "已复制标题链接",
      "Copy Link(accordion)(aria-label)": "复制链接",
      "Copied Link(accordion)(aria-label)": "已复制链接",
      "Skip to Content(skip link)": "跳到主要内容",
    },
  });

function DocumentSearch(props: SharedProps) {
  const locale = useLocale();
  const search = useStaticSearch({ locale });
  return (
    <SearchDialog {...search} {...props}>
      <SearchDialogOverlay />
      <SearchDialogContent className="docs-search-dialog">
        <SearchDialogHeader>
          <SearchDialogIcon />
          <SearchDialogInput />
          <SearchDialogClose />
        </SearchDialogHeader>
        <SearchDialogList />
      </SearchDialogContent>
    </SearchDialog>
  );
}
export function DocsProvider({
  children,
  locale,
}: {
  children: ReactNode;
  locale: Locale;
}) {
  return (
    <RootProvider
      search={{ SearchDialog: DocumentSearch }}
      i18n={{
        ...i18nProvider(translations, locale),
        onLocaleChange: (next) => {
          if (!isLocale(next)) return;
          const url = new URL(location.href);
          url.pathname = switchLocale(url.pathname, next);
          url.searchParams.set("lang", next);
          startNavigation(url.href);
          location.assign(url);
        },
      }}
    >
      {children}
    </RootProvider>
  );
}
