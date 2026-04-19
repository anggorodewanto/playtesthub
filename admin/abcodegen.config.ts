import { type CodegenConfigOptions } from '@accelbyte/codegen'

export default {
  basePath: '',
  shouldProduceIndexFiles: false,
  overrideAsAny: {
    ProtobufAny: true
  }
} satisfies CodegenConfigOptions
