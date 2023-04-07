# Copyright 2023 The Shac Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.


def gosec(ctx, version = "v2.15.0"):
  """Runs gosec on a Go code base.

  See https://github.com/securego/gosec for more details.

  Args:
    ctx: A ctx instance.
    version: gosec version to install. Defaults to a recent version, that will
      be rolled from time to time.
  """
  # TODO(maruel): Always install locally with GOBIN=.tools
  if ctx.os.exec(["go", "install", "github.com/securego/gosec/v2/cmd/gosec@" + version]):
    fail("failed to install")
  if ctx.os.exec(["gosec", "-fmt=golint", "-quiet", "-exclude=G304", "-exclude-dir=.tools", "./..."]):
    # TODO(maruel): Emits lines.
    fail("failed gosec")


def staticcheck(ctx, version = "v0.4.3"):
  """Runs staticcheck on a Go code base.

  See https://github.com/dominikh/go-tools for more details.

  Args:
    ctx: A ctx instance.
    version: staticcheck version to install. Defaults to a recent version, that
    will be rolled from time to time.
  """
  # TODO(maruel): Always install locally with GOBIN=.tools
  if ctx.os.exec(["go", "install", "honnef.co/go/tools/cmd/staticcheck@" + version]):
    fail("failed to install")
  if ctx.os.exec(["staticcheck", "./..."]):
    # TODO(maruel): Emits lines.
    fail("failed staticcheck")
