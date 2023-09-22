# Copyright 2023 The Shac Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

def _print_something(ctx, to_print = "something"):
    print(to_print)

print_hello_check = (
    shac.check(_print_something, name = "print_hello")
        .with_args(to_print = "hello")
)
print_goodbye_check = (
    print_hello_check
        .with_args(to_print = "goodbye")
        .with_name("print_goodbye")
)
print_hello_again_check = (
    print_goodbye_check
        .with_args(to_print = "hello again")
        .with_name("print_hello_again")
)

print("print_hello_check:", print_hello_check)
print("print_goodbye_check:", print_goodbye_check)
print("print_hello_again_check:", print_hello_again_check)

shac.register_check(print_hello_check)
shac.register_check(print_goodbye_check)
shac.register_check(print_hello_again_check)
