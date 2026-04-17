// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.28;

import {Test} from "forge-std/Test.sol";
import {EntryPoint} from "@account-abstraction/contracts/core/EntryPoint.sol";
import {IEntryPoint} from "@account-abstraction/contracts/interfaces/IEntryPoint.sol";
import {SmartAccount} from "../src/SmartAccount.sol";
import {SmartAccountFactory} from "../src/SmartAccountFactory.sol";

contract SmartAccountFactoryTest is Test {
    // ───────────────────────────── State ───────────────────────────────

    EntryPoint public entryPoint;
    SmartAccountFactory public factory;

    address public owner;
    address public otherOwner;

    // ───────────────────────────── Setup ───────────────────────────────

    function setUp() public {
        entryPoint = new EntryPoint();
        factory = new SmartAccountFactory(IEntryPoint(address(entryPoint)));
        owner = makeAddr("owner");
        otherOwner = makeAddr("otherOwner");
    }

    // ───────────────── accountImplementation tests ────────────────────

    function test_accountImplementation_isNonZero() public view {
        assertTrue(address(factory.accountImplementation()) != address(0));
    }

    function test_accountImplementation_hasCode() public view {
        assertTrue(address(factory.accountImplementation()).code.length > 0);
    }

    function test_accountImplementation_entryPointIsCorrect() public view {
        assertEq(address(factory.accountImplementation().entryPoint()), address(entryPoint));
    }

    // ──────────────────── createAccount tests ─────────────────────────

    function test_createAccount_deploysProxy() public {
        SmartAccount account = factory.createAccount(owner, 0);
        assertTrue(address(account).code.length > 0);
    }

    function test_createAccount_initializesOwner() public {
        SmartAccount account = factory.createAccount(owner, 0);
        assertEq(account.owner(), owner);
    }

    function test_createAccount_entryPointIsCorrect() public {
        SmartAccount account = factory.createAccount(owner, 0);
        assertEq(address(account.entryPoint()), address(entryPoint));
    }

    function test_createAccount_deterministicAddress() public {
        address predicted = factory.getAddress(owner, 0);
        SmartAccount account = factory.createAccount(owner, 0);
        assertEq(address(account), predicted);
    }

    function test_createAccount_returnsExistingOnSecondCall() public {
        SmartAccount first = factory.createAccount(owner, 0);
        SmartAccount second = factory.createAccount(owner, 0);
        assertEq(address(first), address(second));
    }

    function test_createAccount_differentSaltDifferentAddress() public {
        SmartAccount a = factory.createAccount(owner, 0);
        SmartAccount b = factory.createAccount(owner, 1);
        assertTrue(address(a) != address(b));
    }

    function test_createAccount_differentOwnerDifferentAddress() public {
        SmartAccount a = factory.createAccount(owner, 0);
        SmartAccount b = factory.createAccount(otherOwner, 0);
        assertTrue(address(a) != address(b));
    }

    function test_createAccount_emitsAccountCreated() public {
        address predicted = factory.getAddress(owner, 0);

        vm.expectEmit(true, true, false, false);
        emit SmartAccountFactory.AccountCreated(predicted, owner);
        factory.createAccount(owner, 0);
    }

    function test_createAccount_noEventOnExistingAccount() public {
        factory.createAccount(owner, 0);

        // Second call returns early — no logs at all.
        vm.recordLogs();
        factory.createAccount(owner, 0);

        assertEq(vm.getRecordedLogs().length, 0);
    }

    // ────────────────────── getAddress tests ──────────────────────────

    function test_getAddress_consistentBeforeAndAfterDeploy() public {
        address before = factory.getAddress(owner, 42);
        factory.createAccount(owner, 42);
        address after_ = factory.getAddress(owner, 42);
        assertEq(before, after_);
    }

    function test_getAddress_differentSaltsDiffer() public view {
        address a = factory.getAddress(owner, 0);
        address b = factory.getAddress(owner, 1);
        assertTrue(a != b);
    }

    function test_getAddress_differentOwnersDiffer() public view {
        address a = factory.getAddress(owner, 0);
        address b = factory.getAddress(otherOwner, 0);
        assertTrue(a != b);
    }

    // ──────────────────── zero-owner guard tests ───────────────────────

    function test_createAccount_revertsZeroOwner() public {
        vm.expectRevert(SmartAccountFactory.InvalidOwner.selector);
        factory.createAccount(address(0), 0);
    }

    function test_getAddress_revertsZeroOwner() public {
        vm.expectRevert(SmartAccountFactory.InvalidOwner.selector);
        factory.getAddress(address(0), 0);
    }

    // ──────────────── proxy initialization guard tests ────────────────

    function test_proxy_cannotBeReinitialised() public {
        SmartAccount account = factory.createAccount(owner, 0);
        vm.expectRevert();
        account.initialize(otherOwner);
    }

    function test_implementation_cannotBeInitialised() public {
        SmartAccount impl = factory.accountImplementation();
        vm.expectRevert();
        impl.initialize(owner);
    }
}
