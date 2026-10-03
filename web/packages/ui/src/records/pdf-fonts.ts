/// <reference path="./pdf-worker-url.d.ts" />
import dingbats from "pdfjs-dist/standard_fonts/FoxitDingbats.pfb?url";
import fixed from "pdfjs-dist/standard_fonts/FoxitFixed.pfb?url";
import fixedBold from "pdfjs-dist/standard_fonts/FoxitFixedBold.pfb?url";
import fixedBoldItalic from "pdfjs-dist/standard_fonts/FoxitFixedBoldItalic.pfb?url";
import fixedItalic from "pdfjs-dist/standard_fonts/FoxitFixedItalic.pfb?url";
import serif from "pdfjs-dist/standard_fonts/FoxitSerif.pfb?url";
import serifBold from "pdfjs-dist/standard_fonts/FoxitSerifBold.pfb?url";
import serifBoldItalic from "pdfjs-dist/standard_fonts/FoxitSerifBoldItalic.pfb?url";
import serifItalic from "pdfjs-dist/standard_fonts/FoxitSerifItalic.pfb?url";
import symbol from "pdfjs-dist/standard_fonts/FoxitSymbol.pfb?url";
import sansBold from "pdfjs-dist/standard_fonts/LiberationSans-Bold.ttf?url";
import sansBoldItalic from "pdfjs-dist/standard_fonts/LiberationSans-BoldItalic.ttf?url";
import sansItalic from "pdfjs-dist/standard_fonts/LiberationSans-Italic.ttf?url";
import sans from "pdfjs-dist/standard_fonts/LiberationSans-Regular.ttf?url";

/** Fixed renderer assets from the installed package, never filenames or URLs supplied by PDF content. */
export const pdfStandardFonts=new Map<string,string>([["FoxitDingbats.pfb",dingbats],["FoxitFixed.pfb",fixed],["FoxitFixedBold.pfb",fixedBold],["FoxitFixedBoldItalic.pfb",fixedBoldItalic],["FoxitFixedItalic.pfb",fixedItalic],["FoxitSerif.pfb",serif],["FoxitSerifBold.pfb",serifBold],["FoxitSerifBoldItalic.pfb",serifBoldItalic],["FoxitSerifItalic.pfb",serifItalic],["FoxitSymbol.pfb",symbol],["LiberationSans-Bold.ttf",sansBold],["LiberationSans-BoldItalic.ttf",sansBoldItalic],["LiberationSans-Italic.ttf",sansItalic],["LiberationSans-Regular.ttf",sans]]);
